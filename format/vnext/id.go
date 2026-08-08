// Package vnext implements the schema-evolved Atlas container beside the
// frozen version 3 format. Its compatibility boundary is field identity and
// wire kind, not a document-wide schema version.
package vnext

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ID is the globally unique identity of a type or field. Blocks use compact
// local directory positions after naming each field once with this full ID.
type ID [16]byte

// IDFromName deterministically allocates an ID inside a namespace. Publishers
// can use a domain they control as the namespace without a central registry.
func IDFromName(namespace, name string) ID {
	digest := sha256.Sum256([]byte(namespace + "\x00" + name))
	var id ID
	copy(id[:], digest[:len(id)])
	// Mark the value as a name-derived UUID and set the RFC 4122 variant.
	id[6] = id[6]&0x0f | 0x50
	id[8] = id[8]&0x3f | 0x80
	return id
}

// CoreID returns an identity in Atlas's own schema namespace.
func CoreID(name string) ID { return IDFromName("dev.atlas.core", name) }

// ParseID accepts the customary dashed form or 32 hexadecimal digits.
func ParseID(value string) (ID, error) {
	var id ID
	plain := strings.ReplaceAll(value, "-", "")
	if len(plain) != hex.EncodedLen(len(id)) {
		return id, fmt.Errorf("ID %q is not 128 bits", value)
	}
	if _, err := hex.Decode(id[:], []byte(plain)); err != nil {
		return ID{}, fmt.Errorf("decode ID %q: %w", value, err)
	}
	return id, nil
}

func (id ID) String() string {
	plain := hex.EncodeToString(id[:])
	return plain[:8] + "-" + plain[8:12] + "-" + plain[12:16] + "-" + plain[16:20] + "-" + plain[20:]
}

func (id ID) isZero() bool { return id == ID{} }

func (id ID) MarshalJSON() ([]byte, error) { return json.Marshal(id.String()) }

func (id *ID) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode ID string: %w", err)
	}
	parsed, err := ParseID(value)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}
