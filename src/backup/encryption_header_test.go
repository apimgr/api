package backup

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncryptedArchiveHeaderUsesSpecifiedParametersAndKeyID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "full.tar.gz.enc")
	_, err := CreateWithOptions(path, CreateOptions{Password: "header-test-password", Kind: KindFull})
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 55)
	require.Equal(t, "BKUP1", string(data[:5]))
	require.Equal(t, uint32(65536), binary.BigEndian.Uint32(data[21:25]))
	require.Equal(t, uint32(3), binary.BigEndian.Uint32(data[25:29]))
	require.Equal(t, uint32(4), binary.BigEndian.Uint32(data[29:33]))
	_, _, _, params, keyID, err := readEncryptionHeader(bytes.NewReader(data))
	require.NoError(t, err)
	require.Equal(t, uint32(3), params.time)
	require.Len(t, keyID, 8)
	matches, err := encryptionPasswordMatches(path, "header-test-password")
	require.NoError(t, err)
	require.True(t, matches)
	matches, err = encryptionPasswordMatches(path, "wrong-password")
	require.NoError(t, err)
	require.False(t, matches)
}

func TestEncryptedArchiveRejectsKDFCeilingsBeforeDerivation(t *testing.T) {
	header := make([]byte, 55)
	copy(header, "BKUP1")
	binary.BigEndian.PutUint32(header[21:25], 1048577)
	binary.BigEndian.PutUint32(header[25:29], 3)
	binary.BigEndian.PutUint32(header[29:33], 4)
	_, _, _, _, _, err := readEncryptionHeader(bytes.NewReader(header))
	require.ErrorContains(t, err, "Argon2id parameters")
}
