package main

/*
#include <pwd.h>
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"os/user"
	"strconv"
	"unsafe"
)

// account is the part of a passwd entry needed to start a session.
type account struct {
	name   string
	uid    uint32
	gid    uint32
	groups []uint32
	home   string
	shell  string
}

// lookupAccount looks up a user through NSS. os/user does not expose the
// login shell, so this calls getpwnam directly.
func lookupAccount(name string) (*account, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))

	pw, err := C.getpwnam(cname)
	if pw == nil {
		if err != nil {
			return nil, fmt.Errorf("getpwnam %s: %w", name, err)
		}
		return nil, fmt.Errorf("unknown user %s", name)
	}

	acct := &account{
		name:  C.GoString(pw.pw_name),
		uid:   uint32(pw.pw_uid),
		gid:   uint32(pw.pw_gid),
		home:  C.GoString(pw.pw_dir),
		shell: C.GoString(pw.pw_shell),
	}
	if acct.shell == "" {
		acct.shell = "/bin/sh"
	}

	u := &user.User{Username: acct.name, Gid: strconv.FormatUint(uint64(acct.gid), 10)}
	gids, err := u.GroupIds()
	if err != nil {
		return nil, fmt.Errorf("groups of %s: %w", name, err)
	}
	for _, g := range gids {
		n, err := strconv.ParseUint(g, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("group id %q: %w", g, err)
		}
		acct.groups = append(acct.groups, uint32(n))
	}

	return acct, nil
}

func lookupGroupID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(g.Gid)
}
