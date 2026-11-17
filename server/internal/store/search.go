package store

import (
 "database/sql/driver"
 "time"

 "modernc.org/sqlite"
 "qimeng.local/next/server/internal/domain"
)

func init(){
 sqlite.MustRegisterDeterministicScalarFunction("qimeng_search_token",2,func(_ *sqlite.FunctionContext,args []driver.Value)(driver.Value,error){text,_:=args[0].(string);token,_:=args[1].(string);if domain.SearchToken(text,token){return int64(1),nil};return int64(0),nil})
 sqlite.MustRegisterDeterministicScalarFunction("qimeng_time_key",1,func(_ *sqlite.FunctionContext,args []driver.Value)(driver.Value,error){text,ok:=args[0].(string);if !ok||text==""{return nil,nil};value,e:=time.Parse(time.RFC3339Nano,text);if e!=nil{return nil,e};return value.UTC().Format("2006-01-02T15:04:05.000000000Z"),nil})
}
