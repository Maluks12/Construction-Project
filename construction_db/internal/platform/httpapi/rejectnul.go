package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// NULText is the 400 a request with a NUL character gets (REG4 review R4).
const NULText = "text must not contain NUL characters"

// RejectNUL refuses a request whose JSON body, query string or path holds a
// NUL: Postgres text can't store one, so it would otherwise be a 500. Invalid
// UTF-8 needs no check: encoding/json replaces it with U+FFFD. Decoded
// values are walked, so an escaped backslash before "u0000" is just text.
func RejectNUL() gin.HandlerFunc {
	return func(c *gin.Context) {
		if queryHasNUL(c.Request.URL.Query()) || paramsHaveNUL(c.Params) {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": NULText})
			return
		}
		if c.Request.Body != nil && c.Request.Body != http.NoBody {
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				WriteUnexpected(c, "read request body", err)
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
			if jsonHasNUL(body) {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": NULText})
				return
			}
		}
		c.Next()
	}
}

func queryHasNUL(q url.Values) bool {
	for k, vs := range q {
		if strings.ContainsRune(k, 0) {
			return true
		}
		for _, v := range vs {
			if strings.ContainsRune(v, 0) {
				return true
			}
		}
	}
	return false
}

func paramsHaveNUL(ps gin.Params) bool {
	for _, p := range ps {
		if strings.ContainsRune(p.Value, 0) {
			return true
		}
	}
	return false
}

// jsonHasNUL: any decoded string (a value or an object key) holds a NUL. A
// body that isn't JSON is left to the handler's own decoder.
func jsonHasNUL(body []byte) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if err != nil {
			return false
		}
		if s, ok := tok.(string); ok && strings.ContainsRune(s, 0) {
			return true
		}
	}
}
