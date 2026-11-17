package migration

import "testing"

func TestOnlyV2AndOriginalEventsAreAccepted(t *testing.T){for _,raw:=range []string{`{"format":"qimeng_backup","schemaVersion":1,"appIdentifier":"legacy","data":{"viewEvents":[]}}`,`{"format":"qimeng_backup","schemaVersion":2,"appIdentifier":"legacy","data":{"mediaStats":[{"viewCount":90}]}}`}{if _,e:=DecodeV2([]byte(raw));e==nil{t.Fatal("unsupported or aggregate-only input accepted")}}
 raw:=[]byte(`{"format":"qimeng_backup","schemaVersion":2,"appIdentifier":"com.qimeng.media","data":{"mediaFiles":[{"recordKey":"synthetic","fileName":"sample.png","mediaType":"image","sizeBytes":1}],"viewEvents":[{"eventId":"legacy:preserve-exactly","recordKey":"synthetic","kind":"open","sessionId":"source-session","startedAt":"2026-01-01T00:00:00.000Z","seconds":null}],"mediaStats":[{"recordKey":"synthetic","viewCount":90}]}}`)
 input,e:=DecodeV2(raw);if e!=nil{t.Fatal(e)};if len(input.Data.ViewEvents)!=1||input.Data.ViewEvents[0].EventID!="legacy:preserve-exactly"||input.Data.ViewEvents[0].Seconds!=nil{t.Fatal("original event identity or null facts changed")}
}
func TestNewerEmptyTagSetWinsAndAmbiguousClocksConflict(t *testing.T){old,newer:=int64(100),int64(200);current:=TagState{Names:[]string{"removed"},UpdatedAtMillis:&old,Source:"pc",Explicit:true};empty:=TagState{Names:[]string{},UpdatedAtMillis:&newer,Source:"phone",Explicit:true};decision:=ResolveTags(current,[]TagState{empty});if decision.Action!="apply"||len(decision.State.Names)!=0{t.Fatal("explicit newer empty set lost")}
 for _,candidate:=range []TagState{{Names:[]string{},Source:"unknown",Explicit:true},{Names:[]string{"other"},UpdatedAtMillis:&old,Source:"equal-time",Explicit:true}}{if ResolveTags(current,[]TagState{candidate}).Action!="conflict"{t.Fatal("unproven latest set was fabricated")}}
}
