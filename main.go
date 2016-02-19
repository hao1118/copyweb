package main

import (
	"os"
	"fmt"
	"strings"
	"net/http"
	"io/ioutil"
	"regexp"
	"strconv"
	"github.com/gorilla/mux"
	"github.com/codegangsta/negroni"
	"hash/crc32"
	"time"
	"github.com/saintfish/chardet"
	"encoding/json"
)

type UrlData struct {
	Etag string
	Type string
}

func Url2Filename(url string) string {
	fn := strings.Replace(url, "/", "#", -1)
	fn = strings.Replace(fn, "?", "$", -1)
	fn = strings.Replace(fn, "\\", "#1", -1)
	fn = strings.Replace(fn, ":", "#2", -1)
	fn = strings.Replace(fn, "*", "#3", -1)
	fn = strings.Replace(fn, "\"", "#4", -1)
	fn = strings.Replace(fn, "<", "(", -1)
	fn = strings.Replace(fn, ">", ")", -1)
	fn = strings.Replace(fn, "|", "%", -1)
	return fn
}

func Fetch(url string) ([]byte, string, bool) {
	timeout := time.Duration(time.Minute)
	client := http.Client{
		Timeout: timeout,
	}
	res, err := client.Get(url)
	if err != nil || res.StatusCode >= 400 {
		return nil, "", false
	}
	defer res.Body.Close()
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, "", false
	}
	return body, res.Header.Get("Content-Type"), true
}

func GetUrls(str string) []string {
	reg := regexp.MustCompile(`(?i)(href=|src=|url\()[^\s>)]+[\s>)]`)
	mats := reg.FindAllString(str, -1)
	urls := []string{}
	for _, m := range mats {
		var url string
		if m[0] == 'h' || m[0] == 'H' {
			url = m[5:len(m) - 1]
		}else {
			url = m[4:len(m) - 1]
		}
		url = strings.Trim(url, `"';`)
		urls = append(urls, url)
	}
	return urls
}

func GetPath(url string) string {
	p := strings.LastIndexByte(url, '?')
	if p > 0 {
		url = url[:p]
	}
	p = strings.LastIndexByte(url, '/')
	if p > 0 {
		return url[:p + 1]
	}
	return "/"
}

func GetParent(url string) string {
	if len(url) > 3 {
		p := strings.LastIndexByte(url[:len(url) - 1], '/')
		if p > 0 {
			return url[:p + 1]
		}
	}
	return "/"
}

func GetContentType(data []byte, url string) string {
	ct := http.DetectContentType(data)
	if strings.Contains(ct, "text/plain") {
		if strings.Contains(url, "css") {
			ct = "text/css"
		}
		if strings.HasSuffix(url, ".js") {
			ct = "text/javascript"
		}
	}
	if strings.Contains(ct, "text/html") {
		dt := chardet.NewHtmlDetector()
		r, err := dt.DetectBest(data)
		if err == nil {
			ct = "text/html;charset=" + r.Charset
		}
	}
	return ct
}

var mapData = make(map[string]UrlData)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("usage: copyweb [dir] [http://weburl(client) or port number(server)]")
		os.Exit(1)
	}
	path := "./" + os.Args[1]
	if strings.HasPrefix(os.Args[2], "http://") {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.MkdirAll(path, 0666)
		}
		GetWeb()
	}else {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Println("dir ", os.Args[1], " does not exist");
			os.Exit(1)
		}
		port, err := strconv.Atoi(os.Args[2])
		if err != nil || port < 1 || port > 65535 {
			fmt.Println("port number wrong.");
			os.Exit(1);
		}
		if jd, err := ioutil.ReadFile(os.Args[1] + ".map"); err == nil {
			if json.Unmarshal(jd, &mapData) != nil {
				fmt.Println("Loading map file failed")
			}
		}
		var r = mux.NewRouter()
		r.HandleFunc("/{url:.*}", SetWeb)
		n := negroni.Classic()
		n.UseHandler(r)
		n.Run(":" + strconv.Itoa(port))
	}
}

func GetWeb() {
	path := "./" + os.Args[1] + "/"
	root := os.Args[2]

	worklist := make(chan []string)
	unseenLinks := make(chan string) // de-duplicated URLs

	go func() {
		worklist <- []string{"/", "/favicon.ico"}
	}()

	w := 0
	n := 0
	f := 0
	// Create 20 crawler goroutines to fetch each unseen link.
	for i := 0; i < 20; i++ {
		go func() {
			for link := range unseenLinks {
				newurls := []string{}
				fmt.Println("Fetching: ", link)
				data, ct, ok := Fetch(root + link)
				if !ok {
					fmt.Println("ERROR: ", link)
					f++
				}else {
					fn := Url2Filename(link)
					ioutil.WriteFile(path + fn, data, 0644)
					n++
					fmt.Println("OK: ", n, link)
					if len(ct) == 0 {
						ct = GetContentType(data, link)
					}
					mapData[link] = UrlData{
						Etag:fmt.Sprintf("%x", crc32.ChecksumIEEE(data)),
						Type:ct,
					}
					if strings.Contains(ct, "text/css") || strings.Contains(ct, "text/html") {
						pt := GetPath(link)
						rawurls := GetUrls(string(data))
						for _, r := range rawurls {
							if r == root || r == root + "/" {
								continue
							}
							if strings.HasPrefix(r, "mailto:") || strings.HasPrefix(r, "#") || strings.HasPrefix(r, "https://") {
								continue
							}
							if strings.HasPrefix(r, "javascript:") || strings.HasPrefix(r, "file://") {
								continue
							}
							if strings.HasPrefix(r, "http://") {
								if len(r) < len(root) || !strings.EqualFold(root, r[:len(root)]) {
									continue
								}
								r = strings.Replace(r, root, "", -1)
							}else {
								up := GetParent(pt)
								for strings.HasPrefix(r, "../") {
									r = up + r[3:]
									up = GetParent(up)
								}
								if !strings.HasPrefix(r, "/") {
									r = pt + r
								}
							}
							f := Url2Filename(r)
							if _, err := os.Stat(path + f); err == nil {
								continue
							}
							newurls = append(newurls, r)
						}
					}
				}
				w--
				go func() { worklist <- newurls }()
			}
		}()
	}

	seen := make(map[string]bool)

	for list := range worklist {
		for _, link := range list {
			if !seen[link] {
				w++
				seen[link] = true
				unseenLinks <- link
			}
		}
		if w <= 0 {
			close(worklist)
		}
	}

	if jd, err := json.Marshal(mapData); err == nil {
		ioutil.WriteFile(os.Args[1] + ".map", jd, 0644)
	}

	fmt.Println("Fetched ", n, "files, ", f, " failed.")
}

func SetWeb(w http.ResponseWriter, r *http.Request) {
	path := "./" + os.Args[1] + "/"
	fn := Url2Filename(r.RequestURI)
	ud, ok := mapData[r.RequestURI]
	if !ok {
		if _, err := os.Stat(path + fn); os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
	}else {
		if ud.Etag == r.Header.Get("If-None-Match") {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	data, err := ioutil.ReadFile(path + fn)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !ok {
		ud.Etag = fmt.Sprintf("%x", crc32.ChecksumIEEE(data))
		ud.Type = GetContentType(data, r.RequestURI)
		mapData[r.RequestURI] = ud
	}
	wh := w.Header()
	wh.Add("Content-Type", ud.Type)
	wh.Add("Content-Length", strconv.Itoa(len(data)))
	wh.Add("Etag", ud.Etag)
	w.Write(data)
}
