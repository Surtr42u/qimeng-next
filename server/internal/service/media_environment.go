package service

import (
 "os"
 "strings"
)

// Decoders need only OS/runtime essentials. Credentials, proxy credentials and
// application configuration must not flow into a process handling media bytes.
// This is environment isolation, not a claim of an operating-system sandbox.
func mediaEnvironment() []string {
 allowed:=map[string]bool{"PATH":true,"SYSTEMROOT":true,"WINDIR":true,"TMPDIR":true,"TMP":true,"TEMP":true,"LANG":true,"LC_ALL":true,"LC_CTYPE":true,"TZ":true,"ANDROID_ROOT":true,"ANDROID_DATA":true,"ANDROID_ART_ROOT":true,"ANDROID_I18N_ROOT":true,"ANDROID_TZDATA_ROOT":true}
 result:=[]string{}
 for _,entry:=range os.Environ() {name,_,ok:=strings.Cut(entry,"=");if ok&&allowed[strings.ToUpper(name)] {result=append(result,entry)}}
 return result
}
