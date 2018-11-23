// Copyright (c) 2018 CommerceBlock Team
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package attestation

import (
	"encoding/hex"
	"log"
	"math"

	confpkg "mainstay/config"
	"mainstay/crypto"

	"github.com/btcsuite/btcd/btcec"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

// AttestClient structure
//
// This struct maintains rpc connection to the main bitcoin client
// It implements all the functionality required to generate new
// attestation addresses and new attestation transactions, as well
// as to combine signatures and send transaction to bitcoin network
//
// The struct stores initial configuration for txid and redeemscript
// It parses the initial script to extract initial pubkeys and uses
// these to generate new addresses from client commitments
//
// The struct includes an optional flag 'signerFlag'
// If this is set to true this client also stores a private key
// and can sign transactions. This option is implemented by
// external tools used to sign transactions or in unit-tests
// In the case that no multisig is used, client must be a signer
//
type AttestClient struct {
	// rpc client connection to main bitcoin client
	MainClient *rpcclient.Client

	// chain config for main bitcoin client
	MainChainCfg *chaincfg.Params

	// fees interface for getting latest / bumping fees
	Fees AttestFees

	// init configuration parameters
	// store information on initial keys and txid
	// required to set chain start and do key tweaking
	txid0     string
	script0   string
	pubkeys   []*btcec.PublicKey
	numOfSigs int

	// states whether Attest Client struct is used for transaction
	// signing or simply for address tweaking and transaction creation
	// in signer case the wallet priv key of the signer is imported
	// in no signer case the wallet priv is a nil pointer
	WalletPriv *btcutil.WIF
}

// NewAttestClient returns a pointer to a new AttestClient instance
// Initially locates the genesis transaction in the main chain wallet
// and verifies that the corresponding private key is in the wallet
func NewAttestClient(config *confpkg.Config, signerFlag ...bool) *AttestClient {
	// optional flag to set attest client as signer
	isSigner := false
	if len(signerFlag) > 0 {
		isSigner = signerFlag[0]
	}

	multisig := config.MultisigScript()
	var pkWif *btcutil.WIF
	if isSigner { // signer case import private key
		// Get initial private key from initial funding transaction of main client
		pk := config.InitPK()
		var errPkWif error
		pkWif, errPkWif = crypto.GetWalletPrivKey(pk)
		if errPkWif != nil {
			log.Printf("Invalid private key %s\n", pk)
			log.Fatal(errPkWif)
		}
		importErr := config.MainClient().ImportPrivKeyRescan(pkWif, "init", false)
		if importErr != nil {
			log.Printf("Could not import initial private key %s\n", pk)
			log.Fatal(importErr)
		}
	} else if multisig == "" {
		log.Fatal("No multisig used - Client must be signer and include private key")
	}

	if multisig != "" { // if multisig attestation, parse pubkeys
		pubkeys, numOfSigs := crypto.ParseRedeemScript(config.MultisigScript())

		// verify our key is one of the multisig keys in signer case
		if isSigner {
			myFound := false
			for _, pub := range pubkeys {
				if pkWif.PrivKey.PubKey().IsEqual(pub) {
					myFound = true
				}
			}
			if !myFound {
				log.Fatal("Client address missing from multisig script")
			}
		}

		return &AttestClient{
			MainClient:   config.MainClient(),
			MainChainCfg: config.MainChainCfg(),
			Fees:         NewAttestFees(config.FeesConfig()),
			txid0:        config.InitTX(),
			script0:      multisig,
			pubkeys:      pubkeys,
			numOfSigs:    numOfSigs,
			WalletPriv:   pkWif}
	}
	return &AttestClient{
		MainClient:   config.MainClient(),
		MainChainCfg: config.MainChainCfg(),
		Fees:         NewAttestFees(config.FeesConfig()),
		txid0:        config.InitTX(),
		script0:      multisig,
		pubkeys:      []*btcec.PublicKey{},
		numOfSigs:    1,
		WalletPriv:   pkWif}
}

// Get next attestation key by tweaking with latest commitment hash
// If attestation client is not a signer, then no key is returned
func (w *AttestClient) GetNextAttestationKey(hash chainhash.Hash) (*btcutil.WIF, error) {

	// in no signer case, client has no key - return nil
	if w.WalletPriv == nil {
		return nil, nil
	}

	// Tweak priv key with the latest commitment hash
	tweakedWalletPriv, tweakErr := crypto.TweakPrivKey(w.WalletPriv, hash.CloneBytes(), w.MainChainCfg)
	if tweakErr != nil {
		return nil, tweakErr
	}

	// Import tweaked priv key to wallet
	// importErr := w.MainClient.ImportPrivKeyRescan(tweakedWalletPriv, hash.String(), false)
	// if importErr != nil {
	// 	return nil, importErr
	// }

	return tweakedWalletPriv, nil
}

// Get next attestation address using the commitment hash provided
// In the multisig case this is generated by tweaking all the original
// of the multisig redeem script used to setup attestation, while in
// the single key - attest client signer case the privkey is used
func (w *AttestClient) GetNextAttestationAddr(key *btcutil.WIF, hash chainhash.Hash) (btcutil.Address, string) {

	// In multisig case tweak all initial pubkeys and import
	// a multisig address to the main client wallet
	if len(w.pubkeys) > 0 {
		var tweakedPubs []*btcec.PublicKey
		hashBytes := hash.CloneBytes()
		for _, pub := range w.pubkeys {
			tweakedPub := crypto.TweakPubKey(pub, hashBytes)
			tweakedPubs = append(tweakedPubs, tweakedPub)
		}

		multisigAddr, redeemScript := crypto.CreateMultisig(tweakedPubs, w.numOfSigs, w.MainChainCfg)

		return multisigAddr, redeemScript
	}

	// no multisig - signer case - use client key
	myAddr, _ := crypto.GetAddressFromPrivKey(key, w.MainChainCfg)
	return myAddr, ""
}

// Method to import address to client rpc wallet and report import error
// This address is required to watch unspent and mempool transactions
// IDEALLY would import the P2SH script as well, but not supported by btcsuite
func (w *AttestClient) ImportAttestationAddr(addr btcutil.Address) error {
	// import address for unspent watching
	importErr := w.MainClient.ImportAddress(addr.String())
	if importErr != nil {
		return importErr
	}

	return nil
}

// Generate a new transaction paying to the tweaked address and add fees
// Transaction inputs are generated using the previous unspent in the wallet
// Fees are calculated using AttestFees interface and RBF flag is set manually
func (w *AttestClient) createAttestation(paytoaddr btcutil.Address, txunspent btcjson.ListUnspentResult) (*wire.MsgTx, error) {
	inputs := []btcjson.TransactionInput{{Txid: txunspent.TxID, Vout: txunspent.Vout}}

	amounts := map[btcutil.Address]btcutil.Amount{paytoaddr: btcutil.Amount(txunspent.Amount * 100000000)}
	msgtx, errCreate := w.MainClient.CreateRawTransaction(inputs, amounts, nil)
	if errCreate != nil {
		return nil, errCreate
	}

	// set replace-by-fee flag
	msgtx.TxIn[0].Sequence = uint32(math.Pow(2, float64(32))) - 3

	feePerByte := w.Fees.GetFee()
	fee := int64(feePerByte * msgtx.SerializeSize())
	msgtx.TxOut[0].Value -= fee

	return msgtx, nil
}

// Create new attestation transaction by removing sigs and bumping fee of existing transaction
// Get latest fees from AttestFees API which has an upper/lower limit on fees
func (w *AttestClient) bumpAttestationFees(msgtx *wire.MsgTx) error {
	// first remove any sigs
	msgtx.TxIn[0].SignatureScript = []byte{}

	// bump fees and calculate fee increment
	prevFeePerByte := w.Fees.GetFee()
	w.Fees.BumpFee()
	feePerByteIncrement := w.Fees.GetFee() - prevFeePerByte

	// increase tx fees by fee difference
	feeIncrement := int64(feePerByteIncrement * msgtx.SerializeSize())
	msgtx.TxOut[0].Value -= feeIncrement

	return nil
}

// Given a commitment hash return the corresponding client private key tweaked
// This method should only be used in the attestation client signer case
func (w *AttestClient) GetKeyFromHash(hash chainhash.Hash) btcutil.WIF {
	if !hash.IsEqual(&chainhash.Hash{}) {
		tweakedKey, _ := crypto.TweakPrivKey(w.WalletPriv, hash.CloneBytes(), w.MainChainCfg)
		return *tweakedKey
	}
	return *w.WalletPriv
}

// Given a commitment hash return the corresponding redeemscript for the particular tweak
func (w *AttestClient) GetScriptFromHash(hash chainhash.Hash) string {
	if !hash.IsEqual(&chainhash.Hash{}) {
		_, redeemScript := w.GetNextAttestationAddr(w.WalletPriv, hash)
		return redeemScript
	}
	return w.script0
}

// Sign transaction using key/redeemscript pair generated by previous attested hash
// This method should only be used in the attestation client signer case
func (w *AttestClient) SignTransaction(hash chainhash.Hash, msgTx wire.MsgTx) (*wire.MsgTx, string, error) {

	// Calculate private key and redeemScript from hash
	key := w.GetKeyFromHash(hash)
	redeemScript := w.GetScriptFromHash(hash)
	// Can't get redeem script from unspent as importaddress P2SH not supported
	// if txunspent.RedeemScript != "" {
	//     redeemScript = txunspent.RedeemScript
	// }

	// sign tx and send signature to main attestation client
	prevTxId := msgTx.TxIn[0].PreviousOutPoint.Hash
	prevTx, errRaw := w.MainClient.GetRawTransaction(&prevTxId)
	if errRaw != nil {
		return nil, "", errRaw
	}

	// Sign transaction
	rawTxInput := btcjson.RawTxInput{prevTxId.String(), 0, hex.EncodeToString(prevTx.MsgTx().TxOut[0].PkScript), redeemScript}
	signedMsgTx, _, errSign := w.MainClient.SignRawTransaction3(&msgTx, []btcjson.RawTxInput{rawTxInput}, []string{key.String()})
	if errSign != nil {
		return nil, "", errSign
	}
	return signedMsgTx, redeemScript, nil
}

// Sign the latest attestation transaction with the combined signatures
func (w *AttestClient) signAttestation(msgtx *wire.MsgTx, sigs [][]byte, hash chainhash.Hash) (*wire.MsgTx, error) {
	// set tx pointer and redeem script
	signedMsgTx := msgtx
	redeemScript := w.GetScriptFromHash(hash)
	if w.WalletPriv != nil { // sign transaction - signer case only
		// sign generated transaction
		var errSign error
		signedMsgTx, redeemScript, errSign = w.SignTransaction(hash, *msgtx)
		if errSign != nil {
			return nil, errSign
		}
	}

	// MultiSig case - combine sigs and create new scriptSig
	if redeemScript != "" {
		mySigs, script := crypto.ParseScriptSig(signedMsgTx.TxIn[0].SignatureScript)
		if len(mySigs) > 0 && len(script) > 0 && hex.EncodeToString(script) == redeemScript {
			combinedSigs := append(mySigs, sigs...)

			// take only numOfSigs required
			combinedScriptSig := crypto.CreateScriptSig(combinedSigs[:w.numOfSigs], script)
			signedMsgTx.TxIn[0].SignatureScript = combinedScriptSig
		} else { // no mySigs - just used received client sigs and script
			if len(sigs) >= w.numOfSigs {
				redeemScriptBytes, _ := hex.DecodeString(redeemScript)
				combinedScriptSig := crypto.CreateScriptSig(sigs[:w.numOfSigs], redeemScriptBytes)
				signedMsgTx.TxIn[0].SignatureScript = combinedScriptSig
			}
		}
	}

	return signedMsgTx, nil
}

// Send the latest attestation transaction
func (w *AttestClient) sendAttestation(msgtx *wire.MsgTx) (chainhash.Hash, error) {

	// send signed attestation
	txhash, errSend := w.MainClient.SendRawTransaction(msgtx, false)
	if errSend != nil {
		return chainhash.Hash{}, errSend
	}

	return *txhash, nil
}

// Verify that an unspent vout is on the tip of the subchain attestations
func (w *AttestClient) verifyTxOnSubchain(txid chainhash.Hash) bool {
	if txid.String() == w.txid0 { // genesis transaction
		return true
	} else { //might be better to store subchain on init and no need to parse all transactions every time
		txraw, err := w.MainClient.GetRawTransaction(&txid)
		if err != nil {
			return false
		}

		prevtxid := txraw.MsgTx().TxIn[0].PreviousOutPoint.Hash
		return w.verifyTxOnSubchain(prevtxid)
	}
	return false
}

// Find the latest unspent vout that is on the tip of subchain attestations
func (w *AttestClient) findLastUnspent() (bool, btcjson.ListUnspentResult, error) {
	unspent, err := w.MainClient.ListUnspent()
	if err != nil {
		return false, btcjson.ListUnspentResult{}, err
	}
	if len(unspent) > 0 {
		for _, vout := range unspent {
			txhash, _ := chainhash.NewHashFromStr(vout.TxID)
			if w.verifyTxOnSubchain(*txhash) { //theoretically only one unspent vout, but check anyway
				return true, vout, nil
			}
		}
	}
	return false, btcjson.ListUnspentResult{}, nil
}

// Find any previously unconfirmed transactions in the client
func (w *AttestClient) getUnconfirmedTx() (bool, chainhash.Hash, error) {
	mempool, err := w.MainClient.GetRawMempool()
	if err != nil {
		return false, chainhash.Hash{}, err
	}
	for _, hash := range mempool {
		if w.verifyTxOnSubchain(*hash) {
			return true, *hash, nil
		}
	}
	return false, chainhash.Hash{}, nil
}
