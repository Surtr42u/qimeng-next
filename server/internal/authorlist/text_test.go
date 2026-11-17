package authorlist

import (
 "strings"
 "testing"
)

func TestDeclaredBlocksSourcesAndNumericMediaAreDistinct(t *testing.T){
 text:="1  Synthetic Studio  合成作者(备注)\n来源  渠道甲  Channel Two\n作品\n2099  合成角色.png\n图组\n\n\n\n2  第二作者\n出处\n渠道乙\n作品\n片段.mp4\n"
 parsed,e:=Parse(text);if e!=nil{t.Fatal(e)};if parsed.Format!="blocks"||len(parsed.Blocks)!=2||len(parsed.Blocks[0].Works)!=2||parsed.Blocks[0].Names[1]!="合成作者"||parsed.Blocks[0].Sources[1]!="Channel Two"{t.Fatal("author/source/work grammar was conflated")}
 exported:=Render(parsed.Blocks);if !strings.Contains(exported,"\n\n\n\n2  第二作者\n")||strings.Count(exported,"来源  ")!=2{t.Fatal("canonical layout changed")};again,e:=Parse(exported);if e!=nil||len(again.Blocks[0].Works)!=2||again.Blocks[0].Sources[1]!="Channel Two"{t.Fatal("export is not round-trip safe")}
}
func TestPlainNamesDoNotInventWorksAndInvalidNamesFail(t *testing.T){
 parsed,e:=Parse("合成甲\n合成乙\n合成甲\n");if e!=nil||parsed.Format!="names"||len(parsed.Blocks)!=2||len(parsed.Blocks[0].Works)!=0{t.Fatal("plain names were treated as media associations")}
 for _,text:=range []string{"[备注]","source.png","https://example.invalid/name","1  [备注]\n作品\na.png"}{if _,e=Parse(text);e==nil{t.Fatalf("invalid author accepted: %q",text)}}
 names,e:=Names("Studio Name / Alias  合成名");if e!=nil||len(names)!=3||IdentityKey(names[0])!="studio_name"{t.Fatal("alias identity normalization changed")}
 withSources,e:=Parse("1  合成名\n来源  Channel A / Channel B\n作品\na.png\n");if e!=nil||len(withSources.Blocks[0].Sources)!=1{t.Fatal("source phrase was split using author alias rules")}
}
func TestWorkMatchingPreservesExtensionsAndOnlyToleratesPageSuffixes(t *testing.T){
 cases:=[]struct{work,file string;want bool}{{"合成作品.png","合成作品.mp4",false},{"合成  作品.PNG","合成作品.png",true},{"合成作品","合成作品 (12).png",true},{"合成作品 (1)","合成作品 (1)(2).png",false},{"合成作品.wmv","合成作品.wmv (1).png",false},{"说明.txt","说明.txt (2).jpg",true},{"合成作品","合成作品 其他.png",false}}
 for _,item:=range cases{if got:=Matches(item.work,item.file);got!=item.want{t.Fatalf("%q / %q: got %v",item.work,item.file,got)}}
}
func TestIndexedLookupRetainsExactThenPageMatchesAndDistinctAssets(t *testing.T){
 index:=BuildIndex([]File{{ID:"page",Name:"合成组 (2).png"},{ID:"exact-a",Name:"合成组.png"},{ID:"video",Name:"合成组.mp4"},{ID:"exact-b",Name:"合成组.png"}})
 exact:=index.Match("合成组.PNG");if len(exact)!=2||exact[0].ID!="exact-a"||exact[1].ID!="exact-b"{t.Fatal("indexed extension matching changed")};all:=index.Match("合成组");if len(all)!=4||all[0].ID!="exact-a"||all[3].ID!="page"{t.Fatal("page matches did not follow exact matches")}
}
