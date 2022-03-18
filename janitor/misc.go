package janitor

import (
	"strings"

	"github.com/Azure/go-autorest/autorest/to"
)

func stringPtrToStringLower(val *string) string {
	return strings.ToLower(to.String(val))
}
func stringToStringLower(val string) string {
	return strings.ToLower(val)
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func stringListAddPrefix(list []string, prefix string) []string {
	ret := []string{}
	for _, val := range list {
		ret = append(ret, prefix+val)
	}
	return ret
}
