package bridge

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum-optimism/optimism/op-e2e/bindings"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/receipts"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/transactions"
	"github.com/ethereum-optimism/optimism/op-e2e/e2eutils/wait"
	"github.com/ethereum-optimism/optimism/op-node/rollup/derive"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/log"
)

type Bridger struct {
	l1RpcUrl string
	l2RpcUrl string

	l1Client *ethclient.Client
	l2Client *ethclient.Client

	l1ChainId *big.Int
	l2ChainId *big.Int

	optimismPortalAddress *common.Address
	optimismPortalABI     *abi.ABI
	optimismPortal        *bindings.OptimismPortal

	l1StandardBridgeAddress *common.Address
	l1StandardBridgeABI     *abi.ABI
	l1StandardBridge        *bindings.L1StandardBridge

	l2StandardBridgeAddress *common.Address
	l2StandardBridgeABI     *abi.ABI
	l2StandardBridge        *bindings.L2StandardBridge

	// l1CrossDomainMessengerAddress *common.Address
	// l1CrossDomainMessengerABI     *abi.ABI
	// l1CrossDomainMessenger        *bindings.L1CrossDomainMessenger

	// l2CrossDomainMessengerAddress *common.Address
	// l2CrossDomainMessengerABI     *abi.ABI
	// l2CrossDomainMessenger        *bindings.L2CrossDomainMessenger
}

func NewBridger(
	ctx context.Context,
	l1RpcUrl,
	l2RpcUrl,
	optimismPortalAddressStr,
	l1StandardBridgeAddressStr,
	l2StandardBridgeAddressStr string,
	// l1CrossDomainMessengerAddressStr,
	// l2CrossDomainMessengerAddressStr
) (*Bridger, error) {
	l1Client, err := ethclient.Dial(l1RpcUrl)
	if err != nil {
		return nil, fmt.Errorf("could not dial l1 rpc at %s: %w", l1RpcUrl, err)
	}

	l2Client, err := ethclient.Dial(l2RpcUrl)
	if err != nil {
		return nil, fmt.Errorf("could not dial l2 rpc at %s: %w", l2RpcUrl, err)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := WaitForChainsStart(timeoutCtx, []*ethclient.Client{l1Client, l2Client}); err != nil {
		return nil, fmt.Errorf("one of the clients has not started: %w", err)
	}

	l1ChainId, err := l1Client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not fetch l1 network id: %w", err)
	}

	l2ChainId, err := l2Client.ChainID(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not fetch l2 network id: %w", err)
	}

	optimismPortalAddress, err := SafeParseAddress(optimismPortalAddressStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse OptimismPortal address: %w", err)
	}
	optimismPortalABI, err := bindings.L1StandardBridgeMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("could not get OptimismPortal abi: %w", err)
	}
	optimismPortal, err := bindings.NewOptimismPortal(optimismPortalAddress, l1Client)
	if err != nil {
		return nil, fmt.Errorf("could not instantiate OptimismPortal contract: %w", err)
	}

	l1StandardBridgeAddress, err := SafeParseAddress(l1StandardBridgeAddressStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse L1StandardBridge address: %w", err)
	}
	l1StandardBridgeABI, err := bindings.L1StandardBridgeMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("could not get L1StandardBridge abi: %w", err)
	}
	l1StandardBridge, err := bindings.NewL1StandardBridge(l1StandardBridgeAddress, l1Client)
	if err != nil {
		return nil, fmt.Errorf("could not instantiate L1StandardBridge contract: %w", err)
	}

	l2StandardBridgeAddress, err := SafeParseAddress(l2StandardBridgeAddressStr)
	if err != nil {
		return nil, fmt.Errorf("could not parse L2StandardBridge address: %w", err)
	}
	l2StandardBridgeABI, err := bindings.L2StandardBridgeMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("could not get l2StandardBridge abi: %w", err)
	}
	l2StandardBridge, err := bindings.NewL2StandardBridge(l2StandardBridgeAddress, l2Client)
	if err != nil {
		return nil, fmt.Errorf("could not instantiate L2StandardBridge contract: %w", err)
	}

	// l1CrossDomainMessengerAddress, err := SafeParseAddress(l1CrossDomainMessengerAddressStr)
	// if err != nil {
	// 	return nil, fmt.Errorf("could not parse L1CrossDomainMessenger address: %w", err)
	// }
	// l1CrossDomainMessengerABI, err := bindings.L1CrossDomainMessengerMetaData.GetAbi()
	// if err != nil {
	// 	return nil, fmt.Errorf("could not get l1CrossDomainMessenger abi: %w", err)
	// }
	// l1CrossDomainMessenger, err := bindings.NewL1CrossDomainMessenger(l1CrossDomainMessengerAddress, l2Client)
	// if err != nil {
	// 	return nil, fmt.Errorf("could not instantiate L1CrossDomainMessenger contract: %w", err)
	// }

	// l2CrossDomainMessengerAddress, err := SafeParseAddress(l2CrossDomainMessengerAddressStr)
	// if err != nil {
	// 	return nil, fmt.Errorf("could not parse L2CrossDomainMessenger address: %w", err)
	// }
	// l2CrossDomainMessengerABI, err := bindings.L2CrossDomainMessengerMetaData.GetAbi()
	// if err != nil {
	// 	return nil, fmt.Errorf("could not get l2CrossDomainMessenger abi: %w", err)
	// }
	// l2CrossDomainMessenger, err := bindings.NewL2CrossDomainMessenger(l2CrossDomainMessengerAddress, l2Client)
	// if err != nil {
	// 	return nil, fmt.Errorf("could not instantiate L2CrossDomainMessenger contract: %w", err)
	// }

	return &Bridger{
		l1RpcUrl: l1RpcUrl,
		l2RpcUrl: l2RpcUrl,

		l1Client: l1Client,
		l2Client: l2Client,

		l1ChainId: l1ChainId,
		l2ChainId: l2ChainId,

		optimismPortalAddress: &optimismPortalAddress,
		optimismPortalABI:     optimismPortalABI,
		optimismPortal:        optimismPortal,

		l1StandardBridgeAddress: &l1StandardBridgeAddress,
		l1StandardBridgeABI:     l1StandardBridgeABI,
		l1StandardBridge:        l1StandardBridge,

		l2StandardBridgeAddress: &l2StandardBridgeAddress,
		l2StandardBridgeABI:     l2StandardBridgeABI,
		l2StandardBridge:        l2StandardBridge,

		// l1CrossDomainMessengerAddress: &l1CrossDomainMessengerAddress,
		// l1CrossDomainMessengerABI:     l1CrossDomainMessengerABI,
		// l1CrossDomainMessenger:        l1CrossDomainMessenger,

		// l2CrossDomainMessengerAddress: &l2CrossDomainMessengerAddress,
		// l2CrossDomainMessengerABI:     l2CrossDomainMessengerABI,
		// l2CrossDomainMessenger:        l2CrossDomainMessenger,
	}, nil
}

func (b *Bridger) BridgeETHFromL1ToL2(ctx context.Context, privateKey *ecdsa.PrivateKey, value *big.Int) error {
	opts, err := bind.NewKeyedTransactorWithChainID(privateKey, b.l1ChainId)
	if err != nil {
		return fmt.Errorf("could not setup transactor: %w", err)
	}
	opts.Value = value

	tx, err := transactions.PadGasEstimate(opts, 1.5, func(opts *bind.TransactOpts) (*types.Transaction, error) {
		return b.l1StandardBridge.BridgeETH(opts, DEFAULT_RECEIVE_DEFAULT_GAS_LIMIT, []byte{})
	})
	if err != nil {
		return fmt.Errorf("could not construct calldata for DepositETH: %w", err)
	}

	receipt, err := wait.ForReceiptOK(ctx, b.l1Client, tx.Hash())
	if err != nil {
		if statusErr, ok := err.(*wait.ReceiptStatusError); ok {
			log.Error("bridgeETH transaction trace", "tx", tx.Hash().Hex(), "trace", statusErr.TxTrace)
			return fmt.Errorf("failure in bridge transaction execution: %w", err)
		} else {
			return fmt.Errorf("failed to get bridge transaction receipt: %w", err)
		}
	}

	log.Info("bridge transaction has been mined successfully", "receipt", receipt)

	transactionDepositedEvent, err := receipts.FindLog(receipt.Logs, b.optimismPortal.ParseTransactionDeposited)
	if err != nil {
		return fmt.Errorf("could not parse OptimismPortal.TransactionDeposited event from the receipt logs: %w", err)
	}

	// The L2 special deposit transaction can be dervied from the TransactionDeposited logs
	depositTx, err := derive.UnmarshalDepositLogEvent(&transactionDepositedEvent.Raw)
	if err != nil {
		return fmt.Errorf("encountered error deriving the deposit transaction type from the OptimismPortal.TransactionDeposited event: %w", err)
	}

	log.Info("Successfully derived the L2 deposit transaction", "depositTx", depositTx)

	receipt, err = wait.ForReceiptOK(ctx, b.l2Client, types.NewTx(depositTx).Hash())
	if err != nil {

		if statusErr, ok := err.(*wait.ReceiptStatusError); ok {
			log.Error("deposit transaction trace", "tx", tx.Hash().Hex(), "trace", statusErr.TxTrace)
			return fmt.Errorf("failure in deposit execution: %w", err)
		} else {
			return fmt.Errorf("found error waiting for deposit receipt: %w", err)
		}
	}

	log.Info("successfully bridged to L2", "receipt", receipt)

	return nil
}
