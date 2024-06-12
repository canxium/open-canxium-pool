package proxy

import (
	"fmt"
	"log"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yuriy0803/open-etc-pool-friends/rpc"
	"github.com/yuriy0803/open-etc-pool-friends/util"
	"golang.org/x/crypto/sha3"
)

const maxBacklog = 10

var two256 = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))

type heightDiffPair struct {
	diff   *big.Int
	height uint64
}

type BlockTemplate struct {
	sync.RWMutex
	Header               string
	Seed                 string
	Target               string
	Difficulty           *big.Int
	Height               uint64
	GetPendingBlockCache *rpc.GetBlockReplyPart
	headers              map[string]heightDiffPair
	tx                   *types.Transaction
}

type Block struct {
	difficulty  *big.Int
	hashNoNonce common.Hash
	nonce       uint64
	mixDigest   common.Hash
	number      uint64
}

func (b Block) Difficulty() *big.Int     { return b.difficulty }
func (b Block) HashNoNonce() common.Hash { return b.hashNoNonce }
func (b Block) Nonce() uint64            { return b.nonce }
func (b Block) MixDigest() common.Hash   { return b.mixDigest }
func (b Block) NumberU64() uint64        { return b.number }

func (s *ProxyServer) fetchTemplate() {
	if s.config.IsOfflineMining() {
		s.fetchTxTemplate(false)
	} else {
		s.fetchBlockTemplate()
	}
}

func (s *ProxyServer) fetchBlockTemplate() {
	rpc := s.rpc()
	t := s.currentBlockTemplate()
	pendingReply, height, diff, err := s.fetchPendingBlock()
	if err != nil {
		log.Printf("Error while refreshing pending block on %s: %s", rpc.Name, err)
		return
	}
	reply, err := rpc.GetWork()
	if err != nil {
		log.Printf("Error while refreshing block template on %s: %s", rpc.Name, err)
		return
	}
	// No need to update, we have fresh job
	if t != nil {
		if t.Header == reply[0] {
			return
		}
		if _, ok := t.headers[reply[0]]; ok {
			return
		}
	}

	pendingReply.Difficulty = util.ToHex(s.config.Proxy.Difficulty)

	newTemplate := BlockTemplate{
		Header:               reply[0],
		Seed:                 reply[1],
		Target:               reply[2],
		Height:               height,
		Difficulty:           big.NewInt(diff),
		GetPendingBlockCache: pendingReply,
		headers:              make(map[string]heightDiffPair),
	}
	// Copy job backlog and add current one
	newTemplate.headers[reply[0]] = heightDiffPair{
		diff:   util.TargetHexToDiff(reply[2]),
		height: height,
	}
	if t != nil {
		for k, v := range t.headers {
			if v.height > height-maxBacklog {
				newTemplate.headers[k] = v
			}
		}
	}
	s.blockTemplate.Store(&newTemplate)
	log.Printf("New block to mine on %s at height %d / %s", rpc.Name, height, reply[0][0:10])

	// Stratum
	if s.config.Proxy.Stratum.Enabled {
		go s.broadcastNewJobs()
	}
}

func (s *ProxyServer) fetchPendingBlock() (*rpc.GetBlockReplyPart, uint64, int64, error) {
	rpc := s.rpc()
	reply, err := rpc.GetPendingBlock()
	if err != nil {
		log.Printf("Error while refreshing pending block on %s: %s", rpc.Name, err)
		return nil, 0, 0, err
	}
	blockNumber, err := strconv.ParseUint(strings.Replace(reply.Number, "0x", "", -1), 16, 64)
	if err != nil {
		log.Println("Can't parse pending block number")
		return nil, 0, 0, err
	}
	blockDiff, err := strconv.ParseInt(strings.Replace(reply.Difficulty, "0x", "", -1), 16, 64)
	if err != nil {
		log.Println("Can't parse pending block difficulty")
		return nil, 0, 0, err
	}
	return reply, blockNumber, blockDiff, nil
}

func (s *ProxyServer) fetchTxTemplate(broadcast bool) {
	_, pendingHeight, _, err := s.fetchPendingBlock()
	if err != nil {
		log.Printf("Error while refreshing pending block number: %s, using offline mode", err)
		pendingHeight = uint64(s.estimatePendingBlockNum())
	}

	subsidy := util.TransactionMiningSubsidy(big.NewInt(int64(pendingHeight)))
	fmt.Printf("Transaction mining subsidy for pending block %d is %s\n", pendingHeight, subsidy.String())
	mineFnSignature := []byte("mining(address)")
	hash := sha3.NewLegacyKeccak256()
	hash.Write(mineFnSignature)
	methodID := hash.Sum(nil)[:4]
	paddedAddress := common.LeftPadBytes(s.config.Coinbase.Bytes(), 32)

	var data []byte
	data = append(data, methodID...)
	data = append(data, paddedAddress...)
	miningTx := types.NewTx(&types.MiningTx{
		ChainID:    big.NewInt(s.config.ChainId),
		Nonce:      s.txNonce,
		GasTipCap:  big.NewInt(0), // this kind of tx is gas free
		GasFeeCap:  big.NewInt(0),
		Gas:        100000,
		From:       s.miner,
		To:         s.config.MiningContract,
		Value:      new(big.Int).Mul(subsidy, big.NewInt(s.config.Difficulty)),
		Data:       data,
		Algorithm:  s.config.Algorithm,
		Difficulty: big.NewInt(s.config.Difficulty),
	})

	newTemplate := BlockTemplate{
		Header:               miningTx.SealHash().String(),
		Seed:                 common.BytesToHash(ethash.SeedHash(miningTx.Nonce())).Hex(),
		Target:               common.BytesToHash(new(big.Int).Div(two256, miningTx.Difficulty()).Bytes()).Hex(),
		Height:               miningTx.Nonce(),
		Difficulty:           miningTx.Difficulty(),
		GetPendingBlockCache: nil,
		headers:              make(map[string]heightDiffPair),
		tx:                   miningTx,
	}

	newTemplate.headers[newTemplate.Header] = heightDiffPair{
		diff:   util.TargetHexToDiff(newTemplate.Target),
		height: miningTx.Nonce(),
	}

	t := s.currentBlockTemplate()

	// No need to update, we have fresh job
	if t != nil {
		if t.Header == newTemplate.Header {
			return
		}
		if _, ok := t.headers[newTemplate.Header]; ok {
			return
		}
	}

	height := miningTx.Nonce()
	if t != nil {
		for k, v := range t.headers {
			if v.height > height-maxBacklog {
				newTemplate.headers[k] = v
			}
		}
	}

	s.blockTemplate.Store(&newTemplate)
	log.Printf("New tx to mine on %s at height %d / %s", s.miner, height, newTemplate.Header[0:10])
	if broadcast && s.config.Proxy.Stratum.Enabled {
		go s.broadcastNewJobs()
	}
}

// Estimate current block number because of unable to get from RPC
// this is an estimate, may not correct
func (s *ProxyServer) estimatePendingBlockNum() int64 {
	now := time.Now().Unix()
	forkTime := 1719110848 // estimate time for block 4204800
	diff := now - int64(forkTime)
	diffInBlock := diff / 6
	return diffInBlock + util.HydroForkBlock.Int64()
}
