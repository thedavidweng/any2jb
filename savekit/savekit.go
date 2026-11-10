// Package savekit describes read-only save operations. It has no console mutation API.
package savekit

import (
 "errors"
 "regexp"
)

var titlePattern = regexp.MustCompile(`^[0-9a-f]{16}$`)
var uidPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Archive struct {
 Space uint32 `json:"space"`
 Type uint32 `json:"type"`
 Rank uint32 `json:"rank"`
 Index uint32 `json:"index"`
 StaticID uint64 `json:"staticId"`
}

type Entry struct {
 Archive
 ID string `json:"id"`
 TitleID string `json:"titleId"`
 UID string `json:"uid"`
 Size string `json:"size"`
}

type Request struct {
 Action string `json:"action"`
 TitleID string `json:"titleId,omitempty"`
 UID string `json:"uid,omitempty"`
 Archive *Archive `json:"archive,omitempty"`
 All bool `json:"all,omitempty"`
}

func (r Request) Validate() error {
 switch r.Action {
 case "accounts","titles","inventory":
  if r.TitleID!="" || r.UID!="" || r.Archive!=nil || r.All {return errors.New("listing accepts no archive target")}
  return nil
 case "inspect","backup","filetime":
 default: return errors.New("unsupported read-only SaveKit action")
 }
 if !titlePattern.MatchString(r.TitleID) || !uidPattern.MatchString(r.UID) {return errors.New("use a 16-digit title ID and 32-digit account ID")}
 if r.All && r.Action=="filetime" {return errors.New("timestamp reads need one archive")}
 if a:=r.Archive; a!=nil && (a.Space>255 || a.Type>7 || a.Rank>1 || a.Index>65535 || a.StaticID>9007199254740991) {return errors.New("invalid save archive")}
 return nil
}

type Result struct {
 Action string `json:"action"`
 Users []string `json:"users,omitempty"`
 Titles []string `json:"titles,omitempty"`
 Inventory []Entry `json:"inventory,omitempty"`
 Output string `json:"output,omitempty"`
 Path string `json:"path,omitempty"`
}
