package regimes

import (
	"crypto/sha256"
	"encoding/json"
)

// SHA256 hashes the decoded data set (whitespace doesn't count); an audit
// export records it (M12 D10).
func (ds *DataSet) SHA256() [32]byte {
	b, err := json.Marshal(ds)
	if err != nil {
		// Every field is plain data; Marshal can't fail on it.
		panic("regimes: hash data set: " + err.Error())
	}
	return sha256.Sum256(b)
}
