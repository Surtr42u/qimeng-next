package service

import (
 "encoding/base64"
 "encoding/json"
 "time"
)

const timestampKey="2006-01-02T15:04:05.000000000Z"
type seekCursor struct{Filter string `json:"f"`;Sort string `json:"s"`;Value string `json:"v"`;ID string `json:"id"`}
func readSeek(encoded,filter,sort string)(seekCursor,error){
 var cursor seekCursor;if encoded==""{return cursor,nil};if len(encoded)>4096{return cursor,Fail(400,"invalid_cursor","分页游标无效。")};raw,e:=base64.RawURLEncoding.DecodeString(encoded);if e!=nil{return cursor,Fail(400,"invalid_cursor","分页游标无效。")};if json.Unmarshal(raw,&cursor)!=nil||cursor.Filter!=digest([]byte(filter))||cursor.Sort!=sort||cursor.ID==""||len(cursor.Value)>2048{return cursor,Fail(400,"invalid_cursor","筛选或排序已变化，请从第一页重新加载。")};return cursor,nil
}
func makeSeek(filter,sort,value,id string)string{raw,_:=json.Marshal(seekCursor{Filter:digest([]byte(filter)),Sort:sort,Value:value,ID:id});return base64.RawURLEncoding.EncodeToString(raw)}
func dateKey(value time.Time)string{return value.UTC().Format(timestampKey)}
