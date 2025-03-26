package bridge

import (
	"context"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
	"github.com/holiman/uint256"
)

var ZeroAddress string = "0x0000000000000000000000000000000000000000"

const DEFAULT_RECEIVE_DEFAULT_GAS_LIMIT uint32 = 200_000

func ParseUint256BigInt(value string) (*big.Int, error) {
	uint, err := uint256.FromDecimal(value)
	if err != nil {
		return nil, fmt.Errorf("could not parse value as valid uint256: %w", err)
	}
	return uint.ToBig(), nil
}

func SafeParseAddress(address string) (common.Address, error) {
	address = strings.ToLower(strings.TrimSpace(address))

	// Check if it's a valid Ethereum address
	if !common.IsHexAddress(address) {
		return common.Address{}, fmt.Errorf("invalid Ethereum address: %s", address)
	}

	// Convert to common.Address type
	parsedAddress := common.HexToAddress(address)

	// Check if it's the zero address
	if parsedAddress == common.HexToAddress("0x0000000000000000000000000000000000000000") {
		return common.Address{}, fmt.Errorf("zero address is not allowed")
	}

	return parsedAddress, nil
}

func WaitForChainsStart(ctx context.Context, clients []*ethclient.Client) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	readyClients := make(map[*ethclient.Client]bool)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for all clients to report block production")

		case <-ticker.C:
			for _, client := range clients {
				// Skip clients that already reported block production
				if readyClients[client] {
					continue
				}

				header, err := client.HeaderByNumber(ctx, nil)
				if err != nil {
					log.Error("received error fetching header", "error", err)
					continue
				}

				if header.Number.Uint64() > 0 {
					readyClients[client] = true
				}
			}

			// If all clients have reported block production, exit
			if len(readyClients) == len(clients) {
				return nil
			}
		}
	}
}
