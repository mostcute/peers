package util

import "strings"

const ErrDialSelf = "dial to self attempted"

func ErrorContains(srcerr, terr string) bool {
	if strings.Contains(srcerr, terr) {
		return true
	}
	return false
}
