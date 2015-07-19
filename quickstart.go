package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v2"
)

// getClient uses a Context and Config to retrieve a Token
// then generate a Client. It returns the generated Client.
func getClient(ctx context.Context, config *oauth2.Config) *http.Client {
	cacheFile, err := tokenCacheFile()
	if err != nil {
		log.Fatalf("Unable to get path to cached credential file. %v", err)
	}
	tok, err := tokenFromFile(cacheFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(cacheFile, tok)
	}
	return config.Client(ctx, tok)
}

// getTokenFromWeb uses Config to request a Token.
// It returns the retrieved Token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}

	tok, err := config.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web %v", err)
	}
	return tok
}

// tokenCacheFile generates credential file path/filename.
// It returns the generated credential path/filename.
func tokenCacheFile() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	tokenCacheDir := filepath.Join(usr.HomeDir, ".credentials")
	os.MkdirAll(tokenCacheDir, 0700)
	return filepath.Join(tokenCacheDir,
		url.QueryEscape("drive-api-quickstart.json")), err
}

// tokenFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// saveToken uses a file path to create a file and store the
// token in it.
func saveToken(file string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", file)
	f, err := os.Create(file)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

type Word struct {
	text       string
	fontSize   int
	confidence int
	isStrong   bool
	isEm       bool
	height     int
	weight     int
	props      map[string]string
}
type ByWeight []Word

func (s ByWeight) Len() int {
	return len(s)
}
func (s ByWeight) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}
func (s ByWeight) Less(i, j int) bool {
	return s[i].weight < s[j].weight
}

func avg(slice []int) float64 {
	if len(slice) == 0 {
		return 0
	}
	sum := 0
	for _, conf := range slice {
		sum += conf
	}
	return float64(sum) / float64(len(slice))
}

func ocrImage(tiffFile string, outputPrefix string) (title string, confScore float64) {
	tessCmd := exec.Command("tesseract", tiffFile, outputPrefix,
		"-l", "deu+eng",
		"-c", "tessedit_create_pdf=1",
		"-c", "tessedit_create_txt=1",
		"-c", "tessedit_create_hocr=1",
		"-c", "hocr_font_info=1")
	log.Printf("Running tesseract: %v\n", tessCmd.Args)
	err := tessCmd.Run()
	if err != nil {
		log.Fatal(err)
	}
	hocr, err := os.Open(outputPrefix + ".hocr")
	if err != nil {
		log.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(hocr)
	if err != nil {
		log.Fatal(err)
	}
	words := make([]Word, 0)
	wordStoplist := make(map[string]bool)
	for _, word := range strings.Split("Mister Herr Frau 8052 8802 Artem Natalia Malyshev Malyshew Malysheva Weinbergstrasse 23 Höhenring Schaffhauserstrasse 547", " ") {
		wordStoplist[word] = true
	}
	doc.Find("#page_1").Find(".ocrx_word").Each(func(i int, s *goquery.Selection) {
		props := make(map[string]string)
		for _, prop := range strings.Split(s.AttrOr("title", ""), ";") {
			nameValue := strings.SplitN(strings.TrimLeft(prop, " "), " ", 2)
			props[nameValue[0]] = nameValue[1]
		}
		fontSize, _ := strconv.Atoi(props["x_fsize"])
		confidence, _ := strconv.Atoi(props["x_wconf"])
		height, _ := strconv.Atoi(strings.Split(props["bbox"], " ")[1])
		isStrong := s.Find("strong").Size() > 0
		isEm := s.Find("em").Size() > 0
		weight := fontSize * 10
		/*if isStrong {
			weight += 5
		}
		if isEm {
			weight += 3
		}*/
		if height > 1400 {
			weight -= 50
		}
		if wordStoplist[s.Text()] {
			weight -= 100
		}
		words = append(words, Word{s.Text(), fontSize, confidence, isStrong, isEm, height, weight, props})
	})
	sort.Stable(sort.Reverse(ByWeight(words)))
	titleMeta, err := os.Create(outputPrefix + "-title.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer titleMeta.Close()

	title = ""
	confidences := make([]int, 0)
	for _, word := range words {
		fmt.Fprintf(titleMeta, "%#v\n", word)
		letters := 0
		for _, roone := range word.text {
			if unicode.IsLetter(roone) || unicode.IsDigit(roone) {
				letters++
				// Yes, count every character
				confidences = append(confidences, word.confidence)
			}
		}
		if letters > 0 && word.confidence > 70 {
			title += word.text + " "
		}
		if len(title) > 80 {
			break
		}
	}
	fmt.Fprintf(titleMeta, "\n%s\n", title)
	confScore = avg(confidences)
	return
}

func main() {
	flag.Parse()
	singleFile := flag.Arg(0)
	ctx := context.Background()

	b, err := ioutil.ReadFile("client_secret.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	config, err := google.ConfigFromJSON(b, drive.DriveScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(ctx, config)

	srv, err := drive.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve drive Client %v", err)
	}

	r, err := srv.Files.List().Q("mimeType = 'application/vnd.google-apps.folder' and title = 'Scanner'").MaxResults(2).Do()
	if len(r.Items) != 1 {
		log.Fatalf("No Scanner folter (%v)", r.Items)
	}
	scannerId := r.Items[0].Id
	log.Printf("Scanner folder: %s\n", scannerId)

	query := fmt.Sprintf("'%s' in parents and mimeType = 'application/vnd.google-apps.folder' and title = 'Processed'", scannerId)
	r, err = srv.Files.List().Q(query).MaxResults(2).Do()
	if len(r.Items) != 1 {
		log.Fatalf("No Processed folder (%v)", r.Items)
	}
	processedId := r.Items[0].Id
	log.Printf("Processed folder: %s\n", processedId)

	query = fmt.Sprintf("'%s' in parents and trashed = false and mimeType = 'application/pdf'", scannerId)
	if singleFile != "" {
		query = query + " and title = '" + singleFile + "'"
	} else {
		query = query + " and starred = false"
	}
	log.Printf("Query: %s", query)
	r, err = srv.Files.List().Q(query).MaxResults(100).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve files.", err)
	}

	if len(r.Items) > 0 {
		for n, i := range r.Items {
			log.Printf("%s (%s) %s %#v\n", i.Title, i.Id, i.MimeType, i.DownloadUrl)
			if i.DownloadUrl == "" {
				continue
			}
			resp, err := client.Get(i.DownloadUrl)
			if err != nil {
				log.Fatalf("Download: %s %v %v\n", resp.Status, err)
			}
			defer resp.Body.Close()
			rawPdf := i.Id + ".pdf"
			out, err := os.Create(rawPdf)
			if err != nil {
				log.Fatalf("Create: %s", err)
			}
			io.Copy(out, resp.Body)
			out.Close()
			tiffFile := i.Id + ".tiff"
			gsCmd := exec.Command(
				"gs", "-dNumRenderingThreads=4", "-dINTERPOLATE", "-sDEVICE=tiff24nc", "-r300",
				"-o", tiffFile, "-c", "100000000", "setvmthreshold", "-f", rawPdf)
			log.Printf("Running ghostscript: %v\n", gsCmd.Args)
			err = gsCmd.Run()
			if err != nil {
				log.Fatal(err)
			}

			var title, outputPrefix string
			rotations := []int{0, 180, 90, 270}
			re := regexp.MustCompile("_rotate([0-9]+)")
			rotateMatch := re.FindStringSubmatch(i.Title)
			if len(rotateMatch) == 2 {
				requestedAngle, _ := strconv.Atoi(rotateMatch[1])
				rotations = []int{requestedAngle}
			}
			for _, angle := range rotations {
				var outputPrefixR, ocrInput string
				if angle == 0 {
					outputPrefixR = "ocr-" + i.Id
					ocrInput = tiffFile
				} else {
					ocrInput = fmt.Sprintf("%s-r%d.tiff", tiffFile, angle)
					outputPrefixR = "ocr-" + i.Id + "-r" + strconv.Itoa(angle)
					rotateCmd := exec.Command("convert", tiffFile, "-rotate", strconv.Itoa(angle), ocrInput)
					log.Printf("Running ImageMagick: %v\n", rotateCmd.Args)
					err = rotateCmd.Run()
					if err != nil {
						log.Fatal(err)
					}
				}
				titleR, confScore := ocrImage(ocrInput, outputPrefixR)
				log.Printf("Confidence: %f for title %s", confScore, titleR)
				goodEnough := confScore > 70
				if goodEnough || len(title) == 0 {
					outputPrefix = outputPrefixR
					title = titleR
				}
				if goodEnough {
					break
				}
			}

			ocrText, err := ioutil.ReadFile(outputPrefix + ".txt")
			if err != nil {
				log.Fatal(err)
			}
			pdfFile, err := os.Open(outputPrefix + ".pdf")
			if err != nil {
				log.Fatal(err)
			}
			defer pdfFile.Close()
			descr := string(ocrText) + "\nSource: " + i.Title + " " + i.Id + "\n" + i.AlternateLink + "\n" + i.CreatedDate
			file_meta := &drive.File{Title: title, Description: descr, MimeType: "application/pdf", CreatedDate: i.CreatedDate, ModifiedDate: i.CreatedDate}
			file_meta.Parents = []*drive.ParentReference{&drive.ParentReference{Id: processedId}}
			insertedFile, err := srv.Files.Insert(file_meta).Media(pdfFile).Ocr(false).Do()
			log.Printf("Inserted: %s %s %s", insertedFile.Id, insertedFile.AlternateLink, title)
			addStar := &drive.File{Labels: &drive.FileLabels{Starred: true}, ModifiedDate: i.CreatedDate}
			_, err = srv.Files.Patch(i.Id, addStar).Do()
			if err != nil {
				log.Fatal(err)
			}
		}
	} else {
		fmt.Print("No files found.")
	}
}
