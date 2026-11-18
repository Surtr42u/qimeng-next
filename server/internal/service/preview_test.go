package service

import (
	"bytes"
	"image"
	"image/color"
	"reflect"
	"testing"
)

func TestRepresentativeFramesPreservePercentagesAndSinglePixelContent(t *testing.T){
	if got:=representativePositions(1000);!reflect.DeepEqual(got,[]int64{350,250,450,150,550,50,650,0}){t.Fatal(got)}
	if got:=representativePositions(0);!reflect.DeepEqual(got,[]int64{0}){t.Fatal(got)}
	frame:=image.NewGray(image.Rect(0,0,10,10))
	if !extremeFrame(frame){t.Fatal("black frame was not skipped")}
	frame.SetGray(9,0,color.Gray{Y:15})
	if extremeFrame(frame){t.Fatal("one visible sampled pixel was discarded")}
	for i:=range frame.Pix{frame.Pix[i]=255}
	if !extremeFrame(frame){t.Fatal("white frame was not skipped")}
	frame.SetGray(0,9,color.Gray{Y:240})
	if extremeFrame(frame){t.Fatal("white threshold erased visible content")}
}

func TestJpegOrientationChangesTheScaledPixelsOnce(t *testing.T){
	exif:=[]byte{255,216,255,225,0,34,'E','x','i','f',0,0,'I','I',42,0,8,0,0,0,1,0,0x12,1,3,0,1,0,0,0,6,0,0,0,0,0,0,0,255,217}
	orientation:=jpegOrientation(bytes.NewReader(exif))
	if orientation!=6{t.Fatalf("EXIF orientation = %d",orientation)}
	small:=image.NewRGBA(image.Rect(0,0,3,2));red:=color.RGBA{R:200,A:255};small.SetRGBA(0,0,red)
	oriented:=orientPreview(small,orientation)
	if oriented.Bounds().Dx()!=2||oriented.Bounds().Dy()!=3||oriented.RGBAAt(1,0)!=red{t.Fatal("portrait rotation lost dimensions or rotated in the wrong direction")}
	for _,broken:=range [][]byte{exif[:16],{255,216,255,225,0,1},bytes.Repeat([]byte{255},1024)}{if got:=jpegOrientation(bytes.NewReader(broken));got!=1{t.Fatal("invalid EXIF changed display orientation")}}
}
