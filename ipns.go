package ipns

import (
	"bytes"
	"fmt"
	"time"

	proto "github.com/gogo/protobuf/proto"
	u "github.com/ipfs/go-ipfs-util"
	ic "github.com/libp2p/go-libp2p-crypto"
	peer "github.com/libp2p/go-libp2p-peer"

	pb "github.com/ipfs/go-ipns/pb"
)

// Create creates a new IPNS entry and signs it with the given private key.
//
// This function does not embed the public key. If you want to do that, use
// `EmbedPublicKey`.
func Create(sk ic.PrivKey, val []byte, seq uint64, eol time.Time) (*pb.IpnsEntry, error) {
	entry := new(pb.IpnsEntry)

	entry.Value = val
	typ := pb.IpnsEntry_EOL
	entry.ValidityType = &typ
	entry.Sequence = proto.Uint64(seq)
	entry.Validity = []byte(u.FormatRFC3339(eol))

	sig, err := sk.Sign(ipnsEntryDataForSig(entry))
	if err != nil {
		return nil, err
	}
	entry.Signature = sig

	return entry, nil
}

// Validates validates the given IPNS entry against the given public key.
func Validate(pk ic.PubKey, entry *pb.IpnsEntry) error {
	// Check the ipns record signature with the public key
	if ok, err := pk.Verify(ipnsEntryDataForSig(entry), entry.GetSignature()); err != nil || !ok {
		return ErrSignature
	}

	// Check that record has not expired
	switch entry.GetValidityType() {
	case pb.IpnsEntry_EOL:
		t, err := u.ParseRFC3339(string(entry.GetValidity()))
		if err != nil {
			return err
		}
		if time.Now().After(t) {
			return ErrExpiredRecord
		}
	default:
		return ErrUnrecognizedValidity
	}
	return nil
}

// EmbedPublicKey embeds the given public key in the given ipns entry. While not
// strictly required, some nodes (e.g., DHT servers) may reject IPNS entries
// that don't embed their public keys as they may not be able to validate them
// efficiently.
func EmbedPublicKey(pk ic.PubKey, entry *pb.IpnsEntry) error {
	id, err := peer.IDFromPublicKey(pk)
	if err != nil {
		return err
	}
	extraced, err := id.ExtractPublicKey()
	if err != nil {
		return err
	}
	if extraced != nil {
		return nil
	}
	pkBytes, err := pk.Bytes()
	if err != nil {
		return err
	}
	entry.PubKey = pkBytes
	return nil
}

// ExtractPublicKey extracts a public key matching `pid` from the IPNS record,
// if possible.
//
// This function returns (nil, nil) when no public key can be extracted and
// nothing is malformed.
func ExtractPublicKey(pid peer.ID, entry *pb.IpnsEntry) (ic.PubKey, error) {
	if entry.PubKey != nil {
		pk, err := ic.UnmarshalPublicKey(entry.PubKey)
		if err != nil {
			return nil, fmt.Errorf("unmarshaling pubkey in record: %s", err)
		}

		expPid, err := peer.IDFromPublicKey(pk)
		if err != nil {
			return nil, fmt.Errorf("could not regenerate peerID from pubkey: %s", err)
		}

		if pid != expPid {
			return nil, ErrPublicKeyMismatch
		}
		return pk, nil
	}

	return pid.ExtractPublicKey()
}

// Compare compares two IPNS entries. It returns:
//
// * -1 if a is older than b
// * 0 if a and b cannot be ordered (this doesn't mean that they are equal)
// * +1 if a is newer than b
//
// It returns an error when either a or b are malformed.
//
// NOTE: It *does not* validate the records, the caller is responsible for calling
// `Validate` first.
//
// NOTE: If a and b cannot be ordered by this function, you can determine their
// order by comparing their serialized byte representations (using
// `bytes.Compare`). You must do this if you are implementing a libp2p record
// validator (or you can just use the one provided for you by this package).
func Compare(a, b *pb.IpnsEntry) (int, error) {
	as := a.GetSequence()
	bs := b.GetSequence()

	if as > bs {
		return 1, nil
	} else if as < bs {
		return -1, nil
	}

	at, err := u.ParseRFC3339(string(a.GetValidity()))
	if err != nil {
		return 0, err
	}

	bt, err := u.ParseRFC3339(string(b.GetValidity()))
	if err != nil {
		return 0, err
	}

	if at.After(bt) {
		return 1, nil
	} else if bt.After(at) {
		return -1, nil
	}

	return 0, nil
}

func ipnsEntryDataForSig(e *pb.IpnsEntry) []byte {
	return bytes.Join([][]byte{
		e.Value,
		e.Validity,
		[]byte(fmt.Sprint(e.GetValidityType())),
	},
		[]byte{})
}