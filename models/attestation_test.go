// Copyright (c) 2018 CommerceBlock Team
// Use of this source code is governed by an MIT
// license that can be found in the LICENSE file.

package models

import (
	"errors"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/stretchr/testify/assert"
)

// Test Attestation high level interface
func TestAttestation(t *testing.T) {
	// test default attestation
	attestationDefault := NewAttestationDefault()

	_, errCommitment := attestationDefault.Commitment()
	assert.Equal(t, errors.New(ErrorCommitmentNotDefined), errCommitment)

	commitmentHash := attestationDefault.CommitmentHash()
	assert.Equal(t, chainhash.Hash{}, commitmentHash)

	hash0, _ := chainhash.NewHashFromStr("1a39e34e881d9a1e6cdc3418b54aa57747106bc75e9e84426661f27f98ada3b7")
	hash1, _ := chainhash.NewHashFromStr("2a39e34e881d9a1e6cdc3418b54aa57747106bc75e9e84426661f27f98ada3b7")
	hash2, _ := chainhash.NewHashFromStr("3a39e34e881d9a1e6cdc3418b54aa57747106bc75e9e84426661f27f98ada3b7")
	root, _ := chainhash.NewHashFromStr("bb088c106b3379b64243c1a4915f72a847d45c7513b152cad583eb3c0a1063c2")
	commitments := []chainhash.Hash{*hash0, *hash1, *hash2}
	commitment, errCommitment := NewCommitment(commitments)
	assert.Equal(t, nil, errCommitment)

	// set commitment to default attestation
	attestationDefault.SetCommitment(commitment)
	commitment2, errCommitment2 := attestationDefault.Commitment()
	assert.Equal(t, nil, errCommitment2)
	assert.Equal(t, commitment, commitment2)

	commitmentHash2 := attestationDefault.CommitmentHash()
	assert.Equal(t, *root, commitmentHash2)

	// test regular attestation
	txid, _ := chainhash.NewHashFromStr("4444e34e881d9a1e6cdc3418b54bb57747106bc75e9e84426661f27f98ada3b7")
	attestation := NewAttestation(*txid, commitment)
	commitment3, errCommitment3 := attestation.Commitment()
	assert.Equal(t, nil, errCommitment3)
	assert.Equal(t, commitment, commitment3)

	commitmentHash3 := attestation.CommitmentHash()
	assert.Equal(t, *root, commitmentHash3)

	// test attestation info
	txRes := btcjson.GetTransactionResult{
		BlockHash: "abcde34e881d9a1e6cdc3418b54bb57747106bc75e9e84426661f27f98ada3b7",
		Time:      int64(1542121293),
		TxID:      "4444e34e881d9a1e6cdc3418b54bb57747106bc75e9e84426661f27f98ada3b7"}
	attestation.UpdateInfo(&txRes)
	attestation.Info.Amount = int64(1)
	assert.Equal(t, AttestationInfo{
		Txid:      "4444e34e881d9a1e6cdc3418b54bb57747106bc75e9e84426661f27f98ada3b7",
		Blockhash: "abcde34e881d9a1e6cdc3418b54bb57747106bc75e9e84426661f27f98ada3b7",
		Amount:    int64(1),
		Time:      int64(1542121293)}, attestation.Info)
}

// Test Attestation BSON interface
func TestAttestationBSON(t *testing.T) {
	hash0, _ := chainhash.NewHashFromStr("1a39e34e881d9a1e6cdc3418b54aa57747106bc75e9e84426661f27f98ada3b7")
	hash1, _ := chainhash.NewHashFromStr("2a39e34e881d9a1e6cdc3418b54aa57747106bc75e9e84426661f27f98ada3b7")
	hash2, _ := chainhash.NewHashFromStr("3a39e34e881d9a1e6cdc3418b54aa57747106bc75e9e84426661f27f98ada3b7")
	root, _ := chainhash.NewHashFromStr("bb088c106b3379b64243c1a4915f72a847d45c7513b152cad583eb3c0a1063c2")
	commitments := []chainhash.Hash{*hash0, *hash1, *hash2}
	commitment, _ := NewCommitment(commitments)

	txid, _ := chainhash.NewHashFromStr("4444e34e881d9a1e6cdc3418b54bb57747106bc75e9e84426661f27f98ada3b7")
	attestation := NewAttestation(*txid, commitment)

	// check root
	commitmentHash := attestation.CommitmentHash()
	assert.Equal(t, *root, commitmentHash)

	// test marshal attestation model
	bytes, errBytes := attestation.MarshalBSON()
	// can't test bytes exactly as there is a time component
	// we do test the reverse though below
	assert.Equal(t, 195, len(bytes))
	assert.Equal(t, nil, errBytes)

	// test unmarshal attestaion model and verify reverse works
	testAttestation := &Attestation{}
	testAttestation.UnmarshalBSON(bytes)
	assert.Equal(t, attestation.Txid, testAttestation.Txid)
	assert.Equal(t, attestation.Confirmed, testAttestation.Confirmed)

	// test attestation model to document
	doc, docErr := GetDocumentFromModel(testAttestation)
	assert.Equal(t, nil, docErr)
	assert.Equal(t, attestation.Txid.String(), doc.Lookup(AttestationTxidName).StringValue())
	assert.Equal(t, attestation.Confirmed, doc.Lookup(AttestationConfirmedName).Boolean())

	// test reverse document to attestation model
	testtestCommitment := &Attestation{}
	docErr = GetModelFromDocument(doc, testtestCommitment)
	assert.Equal(t, nil, docErr)
	assert.Equal(t, attestation.Txid, testtestCommitment.Txid)
	assert.Equal(t, attestation.Confirmed, testtestCommitment.Confirmed)
}
