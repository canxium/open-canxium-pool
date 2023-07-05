package proxy

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/yuriy0803/open-etc-pool-friends/rpc"
	"github.com/yuriy0803/open-etc-pool-friends/util"
)

// A regular expression pattern that matches a string starting with "0x"
// followed by exactly 16 hexadecimal digits (0-9 and a-f)
var noncePattern = regexp.MustCompile("^0x[0-9a-f]{16}$")

// A regular expression pattern that matches a string starting with "0x"
// followed by exactly 64 hexadecimal digits (0-9 and a-f)
var hashPattern = regexp.MustCompile("^0x[0-9a-f]{64}$")

// A regular expression pattern that matches a string consisting of 1 to 200
// characters, each of which is a digit, a letter (upper or lower case), a hyphen, or an underscore
var workerPattern = regexp.MustCompile("^[0-9a-zA-Z-_]{1,200}$")

// Stratum
func (s *ProxyServer) handleLoginRPC(cs *Session, params []string, id string) (bool, *ErrorReply) {
	// Check if params are valid
	if len(params) == 0 {
		return false, &ErrorReply{Code: -1, Message: "Invalid params"}
	}

	// Extract login and worker ID from the first parameter if it contains a dot
	login := params[0]
	if strings.ContainsAny(login, ".") {
		var param = strings.Split(login, ".")
		login = param[0]
		id = param[1]
	}

	// Convert login to lowercase and check if worker ID matches the worker pattern
	login = strings.ToLower(login)
	if !workerPattern.MatchString(id) {
		id = "0"
	}

	// Check if login is a valid hexadecimal address and apply login policy
	if !util.IsValidHexAddress(login) {
		return false, &ErrorReply{Code: -1, Message: "Invalid login"}
	}
	if !s.policy.ApplyLoginPolicy(login, cs.ip) {
		return false, &ErrorReply{Code: -1, Message: "You are blacklisted"}
	}

	// Update session information and register the session
	cs.login = login
	cs.worker = id
	s.registerSession(cs)
	log.Printf("Stratum miner connected %v@%v", login, cs.ip)
	return true, nil
}

// handleGetWorkRPC handles the 'getwork' RPC request from a Stratum miner.
func (s *ProxyServer) handleGetWorkRPC(cs *Session) ([]string, *ErrorReply) {
	// Get the current block template.
	t := s.currentBlockTemplate()
	// If the template is nil or empty or if the proxy server is sick, return an error.
	if t == nil || len(t.Header) == 0 || s.isSick() {
		return nil, &ErrorReply{Code: 0, Message: "Work not ready"}
	}
	// Return a slice of strings with the header, seed, target difficulty, and height of the current block template.
	return []string{t.Header, t.Seed, s.diff, util.ToHex(int64(t.Height))}, nil
}

// Stratum
func (s *ProxyServer) handleTCPSubmitRPC(cs *Session, id string, params []string) (bool, *ErrorReply) {
	// Check if the session is subscribed
	s.sessionsMu.RLock()
	_, ok := s.sessions[cs]
	s.sessionsMu.RUnlock()

	if !ok {
		return false, &ErrorReply{Code: 25, Message: "Not subscribed"}
	}

	// Call the handleSubmitRPC function with the session's login and worker IDs
	return s.handleSubmitRPC(cs, cs.login, cs.worker, params)
}

func (s *ProxyServer) handleSubmitRPC(cs *Session, login, id string, params []string) (bool, *ErrorReply) {
	fmt.Printf("Handle new share submited: %s, %s, %+v\n", login, id, params)
	// Check if the number of parameters is correct
	if len(params) != 3 {
		s.policy.ApplyMalformedPolicy(cs.ip)
		log.Printf("Malformed params from %s@%s %v", login, cs.ip, params)
		return false, &ErrorReply{Code: -1, Message: "Invalid params"}
	}

	// Add "0x" prefix to params if the stratum mode is NiceHash
	stratumMode := cs.stratumMode()
	if stratumMode == NiceHash {
		for i := 0; i < len(params); i++ {
			if !strings.HasPrefix(params[i], "0x") {
				params[i] = "0x" + params[i]
			}
		}
	}

	// Check if the nonce and hashes have the correct format
	if !noncePattern.MatchString(params[0]) || !hashPattern.MatchString(params[1]) || !hashPattern.MatchString(params[2]) {
		s.policy.ApplyMalformedPolicy(cs.ip)
		log.Printf("Malformed PoW result from %s@%s %v", login, cs.ip, params)
		return false, &ErrorReply{Code: -1, Message: "Malformed PoW result"}
	}

	// Process the share in a separate goroutine
	go func(s *ProxyServer, cs *Session, login, id string, params []string) {
		// Get the current block template
		t := s.currentBlockTemplate()

		// Check if the share already exists and if it's valid
		exist, validShare := s.processShare(login, id, cs.ip, t, params, stratumMode != EthProxy)

		// Apply the share policy to determine if the share should be accepted
		ok := s.policy.ApplySharePolicy(cs.ip, !exist && validShare)

		// Handle duplicate share
		if exist {
			log.Printf("Duplicate share from %s@%s %v", login, cs.ip, params)
			if !ok {
				cs.disconnect()
				return
			}
			return
		}

		// Handle invalid share
		if !validShare {
			log.Printf("Invalid share from %s@%s", login, cs.ip)
			s.backend.WriteWorkerShareStatus(login, id, false, true, false)
			// Bad shares limit reached, disconnect the session
			if !ok {
				cs.disconnect()
				return
			}
			return
		}

		// Handle valid share
		if s.config.Proxy.Debug {
			log.Printf("Valid share from %s@%s", login, cs.ip)
		}

		// Apply the policy to determine if the session should be disconnected
		if !ok {
			cs.disconnect()
			return
		}
	}(s, cs, login, id, params)

	return true, nil
}

func (cs *Session) disconnect() {
	cs.conn.Close()
}

// handleGetBlockByNumberRPC returns the pending block cache for the current block template.
// If there is no template available, it returns nil.
func (s *ProxyServer) handleGetBlockByNumberRPC() *rpc.GetBlockReplyPart {
	t := s.currentBlockTemplate()
	var reply *rpc.GetBlockReplyPart
	if t != nil {
		reply = t.GetPendingBlockCache
	}
	return reply
}

// handleUnknownRPC handles an unknown RPC request method.
// It logs an error and returns an error reply.
func (s *ProxyServer) handleUnknownRPC(cs *Session, m string) *ErrorReply {
	log.Printf("Unknown request method %s from %s", m, cs.ip)
	s.policy.ApplyMalformedPolicy(cs.ip)
	return &ErrorReply{Code: -3, Message: "Method not found"}
}
