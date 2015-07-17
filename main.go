package main

// #cgo LDFLAGS: -L /usr/local/lib -ltesseract
// #include "tesseract/capi.h"
// #include <stdlib.h>
import "C"

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"

	"github.com/unbe/go.tesseract"
	"gopkg.in/GeertJohan/go.leptonica.v1"
)

func B(cb C.BOOL) bool {
	return cb != 0
}

type TessWord struct {
	text          string
	confidence    float32
	is_bold       bool
	is_italic     bool
	is_underlined bool
	is_monospace  bool
	is_serif      bool
	is_smallcaps  bool
	pointsize     int
	font_id       int
	font_name     string
}

type ByFontSize []TessWord

func (s ByFontSize) Len() int {
	return len(s)
}
func (s ByFontSize) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s ByFontSize) Less(i, j int) bool {
	return s[i].pointsize < s[j].pointsize
}

func GetWord(ri *C.struct_TessResultIterator) TessWord {
	cWord := C.TessResultIteratorGetUTF8Text(ri, C.RIL_WORD)
	// TESS_API const char* TESS_CALL
	// TessResultIteratorWordFontAttributes(const TessResultIterator* handle,
	// BOOL* is_bold, BOOL* is_italic, BOOL* is_underlined,
	// BOOL* is_monospace, BOOL* is_serif, BOOL* is_smallcaps, int* pointsize, int* font_id);
	is_bold := C.BOOL(0)
	is_italic := C.BOOL(0)
	is_underlined := C.BOOL(0)
	is_monospace := C.BOOL(0)
	is_serif := C.BOOL(0)
	is_smallcaps := C.BOOL(0)
	pointsize := C.int(0)
	font_id := C.int(0)

	font_name := C.TessResultIteratorWordFontAttributes(ri, &is_bold, &is_italic, &is_underlined, &is_monospace, &is_serif, &is_smallcaps, &pointsize, &font_id)
	conf := C.TessResultIteratorConfidence(ri, C.RIL_WORD)

	return TessWord{C.GoString(cWord), float32(conf), B(is_bold), B(is_italic), B(is_underlined), B(is_monospace), B(is_serif), B(is_smallcaps), int(pointsize), int(font_id), C.GoString(font_name)}
}

func main() {
	// get the image to try
	flag.Parse()
	image := flag.Arg(0)

	// print the version
	fmt.Println(tesseract.Version())

	// create new tess instance and point it to the tessdata location. Set language to english.
	tessdata_prefix := os.Getenv("TESSDATA_PREFIX")
	if tessdata_prefix == "" {
		tessdata_prefix = "/usr/local/share"
	}
	t, err := tesseract.NewTess(filepath.Join(tessdata_prefix, "tessdata"), "deu+eng")
	if err != nil {
		log.Fatalf("Error while initializing Tess: %s\n", err)
	}
	defer t.Close()

	fmt.Println("Tess handle: %v", t.Handle())

	// open a new Pix from file with leptonica
	pix, err := leptonica.NewPixFromFile(image)
	if err != nil {
		log.Fatalf("Error while getting pix from file: %s\n", err)
	}
	defer pix.Close() // remember to cleanup

	// set the page seg mode to autodetect
	t.SetPageSegMode(tesseract.PSM_AUTO_OSD)

	// setup a whitelist of all basic ascii
	/*err = t.SetVariable("tessedit_char_whitelist", ` !"#$%&'()*+,-./0123456789:;<=>?@ABCDEFGHIJKLMNOPQRSTUVWXYZ[\]^_abcdefghijklmnopqrstuvwxyz{|}~`+"`")
	if err != nil {
		log.Fatalf("Failed to SetVariable: %s\n", err)
	}*/

	// set the image to the tesseract instance
	t.SetImagePix(pix)

	// retrieve text from the tesseract instance
	fmt.Println(t.Text())

	th := (*C.struct_TessBaseAPI)(t.Handle())
	ri := C.TessBaseAPIGetIterator(th)
	defer C.TessResultIteratorDelete(ri)

	words := make([]TessWord, 0)
	if ri != nil {
		pi := C.TessResultIteratorGetPageIterator(ri)
		for {
			words = append(words, GetWord(ri))
			if C.TessPageIteratorNext(pi, C.RIL_WORD) == C.int(0) {
				break
			}
		}
	}
	sort.Stable(sort.Reverse(ByFontSize(words)))
	for _, word := range words {
		if word.confidence > 75 && len(word.text) >= 3 {
			fmt.Printf("%#v\n", word)
		}
	}

	// t.DumpVariables()
}
