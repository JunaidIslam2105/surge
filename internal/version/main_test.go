// lint:ignore-leak-check
package version

import (
	"go.uber.org/goleak"
	"testing"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
