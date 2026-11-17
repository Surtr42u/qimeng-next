package domain

import "testing"

func TestWorkNumbersSeparatePagesClassesAndFillGaps(t *testing.T){
 names:=[]string{"作品 01 (1).png","作品 01 (2).jpg","作品 03.gif","作品 02.mp4","作品 (2).png"}
 number,width,separator:=Allocate("作品","image",names)
 if number!=2||width!=2||separator!=" "{t.Fatalf("image allocation: %d %d %q",number,width,separator)}
 if got:=WorkFilename("作品",separator,number,width,1,".PNG");got!="作品 02 (1).png"{t.Fatal(got)}
 video,_,_:=Allocate("作品","video",names);if video!=1{t.Fatalf("video hole = %d",video)}
 if _,ok:=ParseWork("作品 (2).png");ok{t.Fatal("bare parenthesis must not occupy work number")}
 w,ok:=ParseWork("朱诺007 (4).png");if !ok||w.Family!="朱诺"||w.Width!=3||w.Separator!=""||w.Page!=4{t.Fatalf("%+v %t",w,ok)}
}
func TestNamingRejectsTraversalAndBrokenEncoding(t *testing.T){
 for _,name:=range []string{"../a.png","a\\b.png","NUL.png","a\x00.png","坏�.png","file. ",""}{if ValidName(name){t.Fatalf("accepted %q",name)}}
 for _,p:=range []string{"../outside","/absolute","a/../b","a//b","C:/private"}{if ValidRelative(p){t.Fatalf("accepted path %q",p)}}
 if !ValidName("生或死+拳皇  女天狗&不知火舞 1 (2).png"){t.Fatal("confirmed naming separators rejected")}
}
