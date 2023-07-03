package janitor

import (
	"strings"

	"github.com/webdevops/go-common/utils/to"
)

func stringPtrToStringLower(val *string) string {
	return strings.ToLower(to.String(val))
}
func stringToStringLower(val string) string {
	return strings.ToLower(val)
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if strings.EqualFold(b, a) {
			return true
		}
	}
	return false
}
