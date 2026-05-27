package status

import (
	"github.com/revolver-sys/xrouter/internal/utun"
)

func ListUTUN() ([]string, error) {
	return utun.List()
}
