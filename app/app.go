package app

import (
	"encoding/json"
	"os"

	abci "github.com/tendermint/tendermint/abci/types"
	cmn "github.com/tendermint/tendermint/libs/common"
	dbm "github.com/tendermint/tendermint/libs/db"
	"github.com/tendermint/tendermint/libs/log"
	tmtypes "github.com/tendermint/tendermint/types"

	bam "github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth"
	"github.com/cosmos/cosmos-sdk/x/bank"
	"github.com/cosmos/cosmos-sdk/x/ibc"
	"github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/staking"
)

const (
	appName = "XpxCosmos"
)

// default home directories for expected binaries
var (
	DefaultCLIHome  = os.ExpandEnv("$HOME/.xpx-cosmos-cli")
	DefaultNodeHome = os.ExpandEnv("$HOME/.xpx-cosmos-d")
)

// Extended ABCI application
type DemocoinApp struct {
	*bam.BaseApp
	cdc *codec.Codec

	// keys to access the substores
	capKeyMainStore    *sdk.KVStoreKey
	capKeyAccountStore *sdk.KVStoreKey
	capKeyPowStore     *sdk.KVStoreKey
	capKeyIBCStore     *sdk.KVStoreKey
	capKeyStakingStore *sdk.KVStoreKey
	keyParams          *sdk.KVStoreKey
	tkeyParams         *sdk.TransientStoreKey

	// keepers
	paramsKeeper        params.Keeper
	feeCollectionKeeper auth.FeeCollectionKeeper
	bankKeeper          bank.Keeper
	ibcMapper           ibc.Mapper
	stakingKeeper         simplestaking.Keeper

	// Manage getting and setting accounts
	accountKeeper auth.AccountKeeper
}

func NewDemocoinApp(logger log.Logger, db dbm.DB) *DemocoinApp {

	// Create app-level codec for txs and accounts.
	var cdc = MakeCodec()

	// Create your application object.
	var app = &DemocoinApp{
		BaseApp:            bam.NewBaseApp(appName, logger, db, auth.DefaultTxDecoder(cdc)),
		cdc:                cdc,
		capKeyMainStore:    sdk.NewKVStoreKey(bam.MainStoreKey),
		capKeyAccountStore: sdk.NewKVStoreKey(auth.StoreKey),
		capKeyPowStore:     sdk.NewKVStoreKey("pow"),
		capKeyIBCStore:     sdk.NewKVStoreKey("ibc"),
		capKeyStakingStore: sdk.NewKVStoreKey(staking.StoreKey),
		keyParams:          sdk.NewKVStoreKey("params"),
		tkeyParams:         sdk.NewTransientStoreKey("transient_params"),
	}

	app.paramsKeeper = params.NewKeeper(app.cdc, app.keyParams, app.tkeyParams)

	// Define the accountKeeper.
	app.accountKeeper = auth.NewAccountKeeper(
		cdc,
		app.capKeyAccountStore,
		app.paramsKeeper.Subspace(auth.DefaultParamspace),
		types.ProtoAppAccount,
	)

	// Add handlers.
	app.bankKeeper = bank.NewBaseKeeper(app.accountKeeper)
	app.coolKeeper = cool.NewKeeper(app.capKeyMainStore, app.bankKeeper, cool.DefaultCodespace)
	app.powKeeper = pow.NewKeeper(app.capKeyPowStore, pow.NewConfig("pow", int64(1)), app.bankKeeper, pow.DefaultCodespace)
	app.ibcMapper = ibc.NewMapper(app.cdc, app.capKeyIBCStore, ibc.DefaultCodespace)
	app.stakingKeeper = simplestaking.NewKeeper(app.capKeyStakingStore, app.bankKeeper, simplestaking.DefaultCodespace)
	app.Router().
		AddRoute("bank", bank.NewHandler(app.bankKeeper)).
		AddRoute("ibc", ibc.NewHandler(app.ibcMapper, app.bankKeeper)).
		AddRoute("simplestaking", simplestaking.NewHandler(app.stakingKeeper))

	// Initialize BaseApp.
	app.SetInitChainer(app.initChainerFn(app.coolKeeper, app.powKeeper))
	app.MountStores(app.capKeyMainStore, app.capKeyAccountStore, app.capKeyPowStore, app.capKeyIBCStore, app.capKeyStakingStore)
	app.SetAnteHandler(auth.NewAnteHandler(app.accountKeeper, app.feeCollectionKeeper))
	err := app.LoadLatestVersion(app.capKeyMainStore)
	if err != nil {
		cmn.Exit(err.Error())
	}

	app.Seal()

	return app
}

// custom tx codec
func MakeCodec() *codec.Codec {
	var cdc = codec.New()
	codec.RegisterCrypto(cdc) // Register crypto.
	sdk.RegisterCodec(cdc)    // Register Msgs
	cool.RegisterCodec(cdc)
	pow.RegisterCodec(cdc)
	bank.RegisterCodec(cdc)
	ibc.RegisterCodec(cdc)
	simplestaking.RegisterCodec(cdc)

	// Register AppAccount
	cdc.RegisterInterface((*auth.Account)(nil), nil)
	cdc.RegisterConcrete(&types.AppAccount{}, "xpx-cosmos/Account", nil)

	cdc.Seal()

	return cdc
}

// custom logic for democoin initialization
// nolint: unparam
func (app *DemocoinApp) initChainerFn(coolKeeper cool.Keeper, powKeeper pow.Keeper) sdk.InitChainer {
	return func(ctx sdk.Context, req abci.RequestInitChain) abci.ResponseInitChain {
		stateJSON := req.AppStateBytes

		genesisState := new(types.GenesisState)
		err := app.cdc.UnmarshalJSON(stateJSON, genesisState)
		if err != nil {
			panic(err) // TODO https://github.com/cosmos/cosmos-sdk/issues/468
			// return sdk.ErrGenesisParse("").TraceCause(err, "")
		}

		for _, gacc := range genesisState.Accounts {
			acc, err := gacc.ToAppAccount()
			if err != nil {
				panic(err) // TODO https://github.com/cosmos/cosmos-sdk/issues/468
				//	return sdk.ErrGenesisParse("").TraceCause(err, "")
			}
			app.accountKeeper.SetAccount(ctx, acc)
		}

		// Application specific genesis handling
		err = cool.InitGenesis(ctx, app.coolKeeper, genesisState.CoolGenesis)
		if err != nil {
			panic(err) // TODO https://github.com/cosmos/cosmos-sdk/issues/468
			//	return sdk.ErrGenesisParse("").TraceCause(err, "")
		}

		err = pow.InitGenesis(ctx, app.powKeeper, genesisState.POWGenesis)
		if err != nil {
			panic(err) // TODO https://github.com/cosmos/cosmos-sdk/issues/468
			//	return sdk.ErrGenesisParse("").TraceCause(err, "")
		}

		return abci.ResponseInitChain{}
	}
}

// Custom logic for state export
func (app *DemocoinApp) ExportAppStateAndValidators() (appState json.RawMessage, validators []tmtypes.GenesisValidator, err error) {
	ctx := app.NewContext(true, abci.Header{})

	// iterate to get the accounts
	accounts := []*types.GenesisAccount{}
	appendAccount := func(acc auth.Account) (stop bool) {
		account := &types.GenesisAccount{
			Address: acc.GetAddress(),
			Coins:   acc.GetCoins(),
		}
		accounts = append(accounts, account)
		return false
	}
	app.accountKeeper.IterateAccounts(ctx, appendAccount)

	genState := types.GenesisState{
		Accounts:    accounts,
		POWGenesis:  pow.ExportGenesis(ctx, app.powKeeper),
		CoolGenesis: cool.ExportGenesis(ctx, app.coolKeeper),
	}
	appState, err = codec.MarshalJSONIndent(app.cdc, genState)
	if err != nil {
		return nil, nil, err
	}
	return appState, validators, nil
}
