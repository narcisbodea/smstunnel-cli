//go:build unix

package cli

import "syscall"

func errnoName(e syscall.Errno) string {
	switch e {
	case syscall.ECONNREFUSED:
		return "ECONNREFUSED"
	case syscall.ECONNRESET:
		return "ECONNRESET"
	case syscall.EHOSTUNREACH:
		return "EHOSTUNREACH"
	case syscall.ENETUNREACH:
		return "ENETUNREACH"
	case syscall.EADDRNOTAVAIL:
		return "EADDRNOTAVAIL"
	case syscall.ETIMEDOUT:
		return "ETIMEDOUT"
	case syscall.EPIPE:
		return "EPIPE"
	case syscall.ECONNABORTED:
		return "ECONNABORTED"
	case syscall.ENETDOWN:
		return "ENETDOWN"
	case syscall.EHOSTDOWN:
		return "EHOSTDOWN"
	case syscall.EADDRINUSE:
		return "EADDRINUSE"
	case syscall.EACCES:
		return "EACCES"
	case syscall.EPERM:
		return "EPERM"
	}
	return ""
}
