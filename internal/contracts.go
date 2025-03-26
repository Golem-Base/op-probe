package internal

import (
	"fmt"

	"github.com/ethereum-optimism/optimism/op-e2e/bindings"
	"github.com/ethereum-optimism/optimism/op-service/predeploys"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

type DepositContracts struct {
	OptimismPortalAddress *common.Address
	OptimismPortalABI     *abi.ABI
	OptimismPortal        *bindings.OptimismPortal

	L1StandardBridgeAddress *common.Address
	L1StandardBridgeABI     *abi.ABI
	L1StandardBridge        *bindings.L1StandardBridge

	L2StandardBridgeAddress *common.Address
	L2StandardBridgeABI     *abi.ABI
	L2StandardBridge        *bindings.L2StandardBridge
}

func NewDepositContracts(l1Client, l2Client *ethclient.Client, optimismPortalAddressHex, l1StandardBridgeAddressHex string) (*DepositContracts, error) {

	optimismPortalAddress, err := SafeParseAddress(optimismPortalAddressHex)
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

	l1StandardBridgeAddress, err := SafeParseAddress(l1StandardBridgeAddressHex)
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

	l2StandardBridgeABI, err := bindings.L2StandardBridgeMetaData.GetAbi()
	if err != nil {
		return nil, fmt.Errorf("could not get l2StandardBridge abi: %w", err)
	}
	l2StandardBridge, err := bindings.NewL2StandardBridge(predeploys.L2StandardBridgeAddr, l2Client)
	if err != nil {
		return nil, fmt.Errorf("could not instantiate L2StandardBridge contract: %w", err)
	}

	return &DepositContracts{
		OptimismPortalAddress: &optimismPortalAddress,
		OptimismPortalABI:     optimismPortalABI,
		OptimismPortal:        optimismPortal,

		L1StandardBridgeAddress: &l1StandardBridgeAddress,
		L1StandardBridgeABI:     l1StandardBridgeABI,
		L1StandardBridge:        l1StandardBridge,

		L2StandardBridgeAddress: &predeploys.L2StandardBridgeAddr,
		L2StandardBridgeABI:     l2StandardBridgeABI,
		L2StandardBridge:        l2StandardBridge,
	}, nil
}
