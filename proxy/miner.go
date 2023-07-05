package proxy

import (
	"encoding/hex"
	"log"
	"math/big"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/yuriy0803/etchash"
	"github.com/yuriy0803/open-etc-pool-friends/util"
)

var (
	maxUint256                             = new(big.Int).Exp(big.NewInt(2), big.NewInt(256), big.NewInt(0))
	ecip1099FBlockClassic uint64           = 11700000 // classic mainnet
	ecip1099FBlockMordor  uint64           = 2520000  // mordor
	uip1FEpoch            uint64           = 22       // ubiq mainnet
	hasher                *etchash.Etchash = nil
)

func (s *ProxyServer) processShare(login, id, ip string, t *BlockTemplate, params []string, stratum bool) (bool, bool) {

	if hasher == nil {
		if s.config.Network == "classic" {
			hasher = etchash.New(&ecip1099FBlockClassic, nil)
		} else if s.config.Network == "mordor" {
			hasher = etchash.New(&ecip1099FBlockMordor, nil)
		} else if s.config.Network == "ubiq" {
			hasher = etchash.New(nil, &uip1FEpoch)
		} else if s.config.Network == "canxium" || s.config.Network == "ethereum" || s.config.Network == "ropsten" || s.config.Network == "ethereumPow" || s.config.Network == "ethereumFair" || s.config.Network == "callisto" || s.config.Network == "etica" || s.config.Network == "expanse" || s.config.Network == "octaspace" || s.config.Network == "universal" {
			hasher = etchash.New(nil, nil)
		} else {
			// unknown network
			log.Printf("Unknown network configuration %s", s.config.Network)
			return false, false
		}
	}

	nonceHex := params[0]
	hashNoNonce := params[1]
	mixDigest := params[2]
	nonce, _ := strconv.ParseUint(strings.Replace(nonceHex, "0x", "", -1), 16, 64)
	shareDiff := s.config.Proxy.Difficulty

	var result common.Hash
	if stratum {
		hashNoNonceTmp := common.HexToHash(params[2])

		mixDigestTmp, hashTmp := hasher.Compute(t.Height, hashNoNonceTmp, nonce)
		params[1] = hashNoNonceTmp.Hex()
		params[2] = mixDigestTmp.Hex()
		hashNoNonce = params[1]
		result = hashTmp
	} else {
		hashNoNonceTmp := common.HexToHash(hashNoNonce)
		mixDigestTmp, hashTmp := hasher.Compute(t.Height, hashNoNonceTmp, nonce)

		// check mixDigest
		if mixDigestTmp.Hex() != mixDigest {
			return false, false
		}
		result = hashTmp
	}

	//this is to stop people in wallet blacklist, from getting shares into the db.
	//rare instances of hacks require letting the hacks waste thier money on occassion
	if !s.policy.ApplyLoginWalletPolicy(login) {
		// check to see if this wallet login is blocked
		log.Printf("Blacklisted wallet share, skipped from %v", login)
		return false, false
		//return codes need work here, a lot of it.
	}

	// Block "difficulty" is BigInt
	// NiceHash "difficulty" is float64 ...
	// diffFloat => target; then: diffInt = 2^256 / target
	shareDiffCalc := util.TargetHexToDiff(result.Hex()).Int64()
	shareDiffFloat := util.DiffIntToFloat(shareDiffCalc)
	if shareDiffFloat < 0.0001 {
		log.Printf("share difficulty too low, %f < %d, from %v@%v", shareDiffFloat, t.Difficulty, login, ip)
		s.backend.WriteWorkerShareStatus(login, id, false, true, false)
		return false, false
	}

	if s.config.Proxy.Debug {
		log.Printf("Difficulty pool/block/share = %d / %d / %d(%f) from %v@%v", shareDiff, t.Difficulty, shareDiffCalc, shareDiffFloat, login, ip)
	}

	h, ok := t.headers[hashNoNonce]
	if !ok {
		log.Printf("Stale share from %v@%v", login, ip)
		return false, false
	}

	// check share difficulty
	shareTarget := new(big.Int).Div(maxUint256, big.NewInt(shareDiff))
	if result.Big().Cmp(shareTarget) > 0 {
		s.backend.WriteWorkerShareStatus(login, id, false, false, true)
		return false, false
	}
	// check target difficulty
	target := new(big.Int).Div(maxUint256, big.NewInt(h.diff.Int64()))
	if result.Big().Cmp(target) <= 0 {
		if s.config.IsOfflineMining() {
			s.txNonce += 1
			s.fetchTxTemplate(true)
			t.tx.SetEthashPow(nonce, common.HexToHash(mixDigest))
			signedTx, err := types.SignTx(t.tx, types.NewLondonSigner(big.NewInt(s.config.ChainId)), s.private)
			if err != nil {
				log.Printf("Failed to sign raw transaction error: %+v", err)
				return false, false
			}

			tx, err := signedTx.MarshalBinary()
			if err != nil {
				log.Printf("Failed to marshal raw transaction error: %+v", err)
				return false, false
			}

			rawTx := "0x" + hex.EncodeToString(tx)
			hash, err := s.rpc().SendRawTransaction(rawTx)
			if err != nil {
				log.Printf("Failed to send raw transaction %v", err)
			}

			exist, err := s.backend.WriteBlock(login, id, params, shareDiff, shareDiffCalc, h.diff.Int64(), h.height, s.hashrateExpiration, rawTx, hash)
			if exist {
				return true, false
			}
			if err != nil {
				log.Println("Failed to insert tx candidate into backend:", err)
			} else {
				log.Printf("Inserted tx %v to backend", h.height)
			}
			log.Printf("Tx found by miner %v@%v at nonce %d, hash %s, rawtx %s", login, ip, h.height, hash, rawTx)
		} else {
			ok, err := s.rpc().SubmitBlock(params)
			if err != nil {
				log.Printf("Block submission failure at height %v for %v: %v", h.height, t.Header, err)
			} else if !ok {
				log.Printf("Block rejected at height %v for %v", h.height, t.Header)
				return false, false
			} else {
				s.fetchBlockTemplate()
				exist, err := s.backend.WriteBlock(login, id, params, shareDiff, shareDiffCalc, h.diff.Int64(), h.height, s.hashrateExpiration, "", "")
				if exist {
					return true, false
				}
				if err != nil {
					log.Println("Failed to insert block candidate into backend:", err)
				} else {
					log.Printf("Inserted block %v to backend", h.height)
				}
				log.Printf("Block found by miner %v@%v at height %d", login, ip, h.height)
			}
		}

	} else {
		exist, err := s.backend.WriteShare(login, id, params, shareDiff, shareDiffCalc, h.height, s.hashrateExpiration)
		if exist {
			return true, false
		}
		if err != nil {
			log.Println("Failed to insert share data into backend:", err)
		}
	}
	s.backend.WriteWorkerShareStatus(login, id, true, false, false)
	return false, true
}
