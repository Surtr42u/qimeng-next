package service

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"io"

	"golang.org/x/image/draw"
)

// Only inspect bounded JPEG metadata before the compressed image. TIFF offsets
// are relative to the TIFF header inside the same APP1 segment.
func jpegOrientation(source io.Reader) int {
	reader := io.LimitReader(source, 512<<10)
	var pair [2]byte
	if _, err := io.ReadFull(reader, pair[:]); err != nil || pair != [2]byte{0xff,0xd8} {return 1}
	for {
		if _, err := io.ReadFull(reader,pair[:]); err != nil || pair[0] != 0xff {return 1}
		marker:=pair[1]
		for marker==0xff {
			if _,err:=io.ReadFull(reader,pair[:1]);err!=nil{return 1}
			marker=pair[0]
		}
		if marker==0xda||marker==0xd9{return 1}
		if marker==0x01||marker>=0xd0&&marker<=0xd7{continue}
		if _,err:=io.ReadFull(reader,pair[:]);err!=nil{return 1}
		size:=int(binary.BigEndian.Uint16(pair[:]))-2
		if size<0{return 1}
		if marker!=0xe1 {
			if _,err:=io.CopyN(io.Discard,reader,int64(size));err!=nil{return 1}
			continue
		}
		data:=make([]byte,size)
		if _,err:=io.ReadFull(reader,data);err!=nil{return 1}
		if !bytes.HasPrefix(data,[]byte("Exif\x00\x00")){continue}
		return tiffOrientation(data[6:])
	}
}

func tiffOrientation(data []byte) int {
	if len(data)<8{return 1}
	var order binary.ByteOrder
	switch string(data[:2]) {case "II":order=binary.LittleEndian;case "MM":order=binary.BigEndian;default:return 1}
	if order.Uint16(data[2:4])!=42{return 1}
	offset:=int64(order.Uint32(data[4:8]))
	if offset<8||offset+2>int64(len(data)){return 1}
	count:=int64(order.Uint16(data[offset:offset+2]))
	for at:=offset+2;count>0&&at+12<=int64(len(data));at,count=at+12,count-1{
		entry:=data[at:at+12]
		if order.Uint16(entry[:2])==0x0112&&order.Uint16(entry[2:4])==3&&order.Uint32(entry[4:8])==1{
			value:=int(order.Uint16(entry[8:10]))
			if value>=1&&value<=8{return value}
		}
	}
	return 1
}

// Transform the scaled preview instead of allocating a second full-sized photo.
func orientPreview(src *image.RGBA, orientation int) *image.RGBA {
	if orientation<=1||orientation>8{return src}
	w,h:=src.Bounds().Dx(),src.Bounds().Dy()
	size:=image.Rect(0,0,w,h)
	if orientation>=5 {size=image.Rect(0,0,h,w)}
	dst:=image.NewRGBA(size)
	for y:=0;y<h;y++{
		for x:=0;x<w;x++{
			var dx,dy int
			switch orientation{
			case 2: dx,dy=w-1-x,y
			case 3: dx,dy=w-1-x,h-1-y
			case 4: dx,dy=x,h-1-y
			case 5: dx,dy=y,x
			case 6: dx,dy=h-1-y,x
			case 7: dx,dy=h-1-y,w-1-x
			case 8: dx,dy=y,w-1-x
			}
			dst.SetRGBA(dx,dy,src.RGBAAt(x,y))
		}
	}
	return dst
}

func (s *Service) scaledStill(source mediaInput,mime string,edge,quality int,scale draw.Scaler)([]byte,error){
	f,err:=source.reader(s);if err!=nil{return nil,err};defer f.Close()
	orientation:=1
	if mime=="image/jpeg"{
		orientation=jpegOrientation(f)
		if _,err=f.Seek(0,io.SeekStart);err!=nil{return nil,err}
	}
	cfg,_,err:=image.DecodeConfig(f)
	if err!=nil{return nil,Fail(422,"thumbnail_decode_failed","无法读取图片尺寸。")}
	if cfg.Height<1||cfg.Width<1||int64(cfg.Width)>40_000_000/int64(cfg.Height){return nil,Fail(422,"thumbnail_resource_limit","图片超过解码预算，原件可正常读取。")}
	if _,err=f.Seek(0,io.SeekStart);err!=nil{return nil,err}
	src,_,err:=image.Decode(f);if err!=nil{return nil,Fail(422,"thumbnail_decode_failed","无法解码预览，原件保留。")}
	width,height:=src.Bounds().Dx(),src.Bounds().Dy()
	if width>edge||height>edge{
		if width>=height{height=max(1,height*edge/width);width=edge}else{width=max(1,width*edge/height);height=edge}
	}
	dst:=image.NewRGBA(image.Rect(0,0,width,height))
	scale.Scale(dst,dst.Bounds(),src,src.Bounds(),draw.Src,nil)
	var output bytes.Buffer
	if err=jpeg.Encode(&output,orientPreview(dst,orientation),&jpeg.Options{Quality:quality});err!=nil{return nil,err}
	return output.Bytes(),nil
}
