package internal

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

const ZeroAddressString string = "0x0000000000000000000000000000000000000000"

var ZeroAddress common.Address = common.HexToAddress(ZeroAddressString)

const RECEIVE_DEFAULT_GAS_LIMIT uint32 = 200_000

func ParseUint256BigInt(value string) (*big.Int, error) {
	uint, err := uint256.FromDecimal(value)
	if err != nil {
		return nil, fmt.Errorf("could not parse value as valid uint256: %w", err)
	}
	return uint.ToBig(), nil
}

func SafeParseAddress(addressHex string) (common.Address, error) {
	addressHex = strings.ToLower(strings.TrimSpace(addressHex))
	if !common.IsHexAddress(addressHex) {
		return common.Address{}, fmt.Errorf("invalid Ethereum address: %s", addressHex)
	}

	address := common.HexToAddress(addressHex)
	if address == common.HexToAddress("0x0000000000000000000000000000000000000000") {
		return common.Address{}, fmt.Errorf("zero address is not allowed")
	}

	return address, nil
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

func ConnectClient(ctx context.Context, rpcUrl string) (*ethclient.Client, *big.Int, error) {
	client, err := ethclient.Dial(rpcUrl)
	if err != nil {
		return nil, nil, fmt.Errorf("could not dial rpc url at %s: %w", rpcUrl, err)
	}

	log.Info("Successfully dialed client", "url", rpcUrl)

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := WaitForChainsStart(timeoutCtx, []*ethclient.Client{client}); err != nil {
		return nil, nil, fmt.Errorf("client has not started: %w", err)
	}

	chainId, err := client.ChainID(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("could not fetch l1 network id: %w", err)
	}

	log.Info("Successfully connected to chain", "chainId", chainId)

	return client, chainId, nil
}
