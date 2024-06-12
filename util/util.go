package util

import (
	"encoding/hex"
	"math/big"
	"regexp"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
)

var Ether = math.BigPow(10, 18)
var Shannon = math.BigPow(10, 9)

var pow256 = math.BigPow(2, 256)
var addressPattern = regexp.MustCompile("^0x[0-9a-fA-F]{40}$")
var zeroHash = regexp.MustCompile("^0?x?0+$")

var Diff1 = StringToBig("0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF")
var big0 = big.NewInt(0)

var (
	CanxiumRewardPerHash = big.NewInt(250) // Reward in wei per difficulty hash for successfully mining upward from Canxium
	// offline mining
	CanxiumMaxTransactionReward = big.NewInt(4250)
	CanxiumMiningReduceBlock    = big.NewInt(432000) // Offline mining reward reduce 11.76% every 432000 blocks
	CanxiumMiningReducePeriod   = big.NewInt(24)     // Max 24 months
	CanxiumMiningPeriodPercent  = big.NewInt(8842)

	HydroForkBlock = big.NewInt(4204800)
)

func StringToBig(h string) *big.Int {
	n := new(big.Int)
	n.SetString(h, 0)
	return n
}

func IsValidHexAddress(s string) bool {
	if IsZeroHash(s) || !addressPattern.MatchString(s) {
		return false
	}
	return true
}

func IsZeroHash(s string) bool {
	return zeroHash.MatchString(s)
}

func MakeTimestamp() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func GetTargetHex(diff int64) string {
	difficulty := big.NewInt(diff)
	diff1 := new(big.Int).Div(Diff1, difficulty)
	targetBytes := diff1.Bytes()
	if len(targetBytes) < 32 {
		padding := make([]byte, 32-len(targetBytes))
		targetBytes = append(padding, targetBytes...)
	}
	return "0x" + hex.EncodeToString(targetBytes)
}

func TargetHexToDiff(targetHex string) *big.Int {
	targetBytes := common.FromHex(targetHex)
	return new(big.Int).Div(pow256, new(big.Int).SetBytes(targetBytes))
}

func ToHex(n int64) string {
	return "0x0" + strconv.FormatInt(n, 16)
}

func FormatReward(reward *big.Int) string {
	return reward.String()
}

func FormatRatReward(reward *big.Rat) string {
	wei := new(big.Rat).SetInt(Ether)
	reward = reward.Quo(reward, wei)
	return reward.FloatString(8)
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func MustParseDuration(s string) time.Duration {
	value, err := time.ParseDuration(s)
	if err != nil {
		panic("util: Can't parse duration `" + s + "`: " + err.Error())
	}
	return value
}

func String2Big(num string) *big.Int {
	n := new(big.Int)
	n.SetString(num, 0)
	return n
}

func DiffFloatToInt(diffFloat float64) (diffInt int64) {
	diffInt = int64(diffFloat * float64(1<<48) / float64(0xffff)) // 48 = 256 - 26*8
	return
}

func DiffIntToFloat(diffInt int64) (diffFloat float64) {
	diffFloat = float64(diffInt*0xffff) / float64(1<<48) // 48 = 256 - 26*8
	return
}

func ToHex1(n int64) string {
	return strconv.FormatInt(n, 10)
}

// https://github.com/octanolabs/go-spectrum/blob/21ca5a2f3fec6c4bd12d5cc0a93b40cd305036fc/util/util.go
func DecodeValueHex(val string) string {

	if len(val) < 2 || val == "0x0" {
		return "0"
	}

	if val[:2] == "0x" {
		x, err := hexutil.DecodeBig(val)

		if err != nil {
			//		log.Error("errorDecodeValueHex", "str", val, "err", err)
		}
		return x.String()
	} else {
		x, ok := big.NewInt(0).SetString(val, 16)

		if !ok {
			//		log.Error("errorDecodeValueHex", "str", val, "ok", ok)
		}

		return x.String()
	}
}

func TransactionMiningSubsidy(block *big.Int) *big.Int {
	blockPassed := new(big.Int).Sub(block, HydroForkBlock)
	period := new(big.Int).Div(blockPassed, CanxiumMiningReduceBlock)
	if period.Cmp(big0) == 0 {
		return CanxiumMaxTransactionReward
	}

	// reduce mining reward for max 24 period
	if period.Cmp(CanxiumMiningReducePeriod) >= 0 {
		return CanxiumRewardPerHash
	}

	exp := new(big.Int).Exp(CanxiumMiningPeriodPercent, period, nil)
	percentage := new(big.Int).Exp(big.NewInt(10000), period, nil)
	periodReward := new(big.Int).Mul(CanxiumMaxTransactionReward, exp)
	subsidy := new(big.Int).Div(periodReward, percentage)
	return subsidy
}
