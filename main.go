package main

import (
	"os"
	"flag"
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

func fetch(url string) ([]byte, string, bool) {
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

func geturls(str string) []string {
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

func getcurpath(url string) string {
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

func getuppath(url string) string {
	if len(url) > 3 {
		p := strings.LastIndexByte(url[:len(url) - 1], '/')
		if p > 0 {
			return url[:p + 1]
		}
	}
	return "/"
}

func getContentType(data []byte, url string) string {
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
	flag.Parse()
	args := flag.Args()
	if len(args) != 2 {
		fmt.Println("usage: copyweb [dir] [http://weburl(client) or port number(server)]")
		os.Exit(1)
	}
	path := "./" + args[0]
	if strings.HasPrefix(args[1], "http://") {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			os.MkdirAll(path, 0666)
		}
		getweb(args[1])
	}else {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			fmt.Println("dir ", args[0], " does not exist");
			os.Exit(1)
		}
		port, err := strconv.Atoi(args[1])
		if err != nil || port < 1 || port > 65535 {
			fmt.Println("port number wrong.");
			os.Exit(1);
		}
		if jd, err := ioutil.ReadFile(args[0] + ".map"); err == nil {
			if json.Unmarshal(jd, mapData) != nil {
				fmt.Println("Loading map file failed")
			}
		}
		var r = mux.NewRouter()
		r.HandleFunc("/{url:.*}", setweb)
		n := negroni.Classic()
		n.UseHandler(r)
		n.Run(":" + strconv.Itoa(port))
	}
}

func getweb(root string) {
	path := "./" + flag.Arg(0) + "/"
	urls := make(map[string]int)
	urls["/"] = 0
	urls["/favicon.ico"] = 0
	n := 0
	for {
		if len(urls) == 0 {
			break
		}
		fmt.Println("Tasks: ", len(urls))
		for k, v := range urls {
			if v > 3 {
				fmt.Println("error: ", k)
				delete(urls, k)
				continue
			}
			fmt.Println("Fetching: ", k)
			data, ct, ok := fetch(root + k)
			if !ok {
				urls[k] = v + 1
				fmt.Println("Fetch failed:", k)
				continue
			}
			fn := Url2Filename(k)
			ioutil.WriteFile(path + fn, data, 0644)
			n++
			if len(ct) == 0 {
				ct = getContentType(data, k)
			}
			mapData[k] = UrlData{
				Etag:fmt.Sprintf("%x", crc32.ChecksumIEEE(data)),
				Type:ct,
			}
			fmt.Println("Saved: ", n, "\t", k)
			delete(urls, k)
			if strings.Contains(ct, "text/css") || strings.Contains(ct, "text/html") {
				pt := getcurpath(k)
				rawurls := geturls(string(data))
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
						up := getuppath(pt)
						for strings.HasPrefix(r, "../") {
							r = up + r[3:]
							up = getuppath(up)
						}
						if !strings.HasPrefix(r, "/") {
							r = pt + r
						}
					}
					f := Url2Filename(r)
					if _, err := os.Stat(path + f); err == nil {
						continue
					}
					urls[r] = 0
					fmt.Println("Add url: ", pt, "->", r)
				}
			}
		}
	}
	if jd, err := json.Marshal(mapData); err == nil {
		ioutil.WriteFile(flag.Arg(0) + ".map", jd, 0644)
	}
	fmt.Println("Fetched ", n, "files.")
}

func setweb(w http.ResponseWriter, r *http.Request) {
	path := "./" + flag.Arg(0) + "/"
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
		ud.Type = getContentType(data, r.RequestURI)
		mapData[r.RequestURI] = ud
	}
	wh := w.Header()
	wh.Add("Content-Type", ud.Type)
	wh.Add("Content-Length", strconv.Itoa(len(data)))
	wh.Add("Etag", ud.Etag)
	w.Write(data)
}
