package main

import (
	"embed"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
)

//go:embed src/*gohtml tmpl/*gohtml tmpl/**/*gohtml
var UrlFS embed.FS

//go:embed static/*
var staticFS embed.FS

//go:embed static/img/favicon.svg
var faviconBytes []byte

var (
	Mux    *http.ServeMux
	Config struct {
		IsEmbed bool
		Context struct {
			Site map[string]any
		}
	}
)

func init() {
	Mux = http.NewServeMux()
}

func init() {
	isEmbed := flag.Bool("e", false, "True: embed, false: filesystem")
	flag.Parse()
	Config.IsEmbed = *isEmbed
	Config.Context.Site = map[string]any{"Title": "Demo"}

}

func Dict(values ...any) (map[string]any, error) {
	if len(values)%2 != 0 {
		return nil, errors.New("parameters must be even")
	}
	dict := make(map[string]any)
	var key, val any
	for {
		key, val, values = values[0], values[1], values[2:]
		switch reflect.ValueOf(key).Kind() {
		case reflect.String:
			dict[key.(string)] = val
		default:
			return nil, errors.New(`type must equal to "string"`)
		}
		if len(values) == 0 {
			break
		}
	}
	return dict, nil
}

func CollectFilesFromFS(fs embed.FS, dirName string, isRecursive bool) (filepathList []string, err error) {
	dirEntryList, err := fs.ReadDir(dirName)
	if err != nil {
		return nil, err
	}

	for _, dirEntry := range dirEntryList {
		if dirEntry.IsDir() {
			if isRecursive {
				fpList, _ := CollectFilesFromFS(fs, path.Join(dirName, dirEntry.Name()), isRecursive)
				filepathList = append(filepathList, fpList...)
			}
			continue
		}
		filepathList = append(filepathList, path.Join(dirName, dirEntry.Name()))
	}
	return
}

func CollectFiles(dir string) (filepathList []string, err error) {
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		filepathList = append(filepathList, path)
		return nil
	})
	return
}

func initURL() {
	Mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "image/svg+xml")
		_, _ = w.Write(faviconBytes)
	})

	Mux.Handle("/static/", http.FileServer(http.FS(staticFS)))

	var (
		tmplFileList []string
		err          error
	)
	if Config.IsEmbed {
		tmplFileList, err = CollectFilesFromFS(UrlFS, "tmpl", true)
	} else {
		tmplFileList, err = CollectFiles("./tmpl")
	}
	if err != nil {
		panic(err)
	}
	re := regexp.MustCompile(`{{-? ?template \"(?P<Name>[^() ]*)\" ?.* ?-?}}`)
	Mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		curSrc := path.Join("./src", r.URL.Path)
		switch filepath.Ext(r.URL.Path) {
		case "":
			curSrc = path.Join(curSrc, "index.html")
			fallthrough
		case ".html": // treat all HTML as GoHTML
			curSrc = curSrc[:len(curSrc)-4] + "gohtml"
			fallthrough
		case ".gohtml":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			var content []byte
			if Config.IsEmbed {
				content, err = UrlFS.ReadFile(curSrc)
			} else {
				content, err = os.ReadFile(curSrc)
			}
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			matchList := re.FindAllStringSubmatch(string(content), -1)
			tmplMap := make(map[string][]string)
			for _, match := range matchList {
				for idxGroup, name := range re.SubexpNames() {
					if idxGroup != 0 && name != "" {
						tmplMap[name] = append(tmplMap[name], match[idxGroup])
					}
				}
			}
			setTmpl := map[string]string{}
			for _, tmpl := range tmplMap["Name"] {
				if _, exists := setTmpl[tmpl]; exists {
					continue
				}
				setTmpl[tmpl] = tmpl
			}
			var filterTmpl []string
			for _, tmplFilepath := range tmplFileList {
				_, exists := setTmpl[filepath.Base(tmplFilepath)]
				if exists {
					filterTmpl = append(filterTmpl, tmplFilepath)
				}
			}
			filterTmpl = append(filterTmpl, curSrc)
			fmt.Printf("Templates used on this page:%+v\n", filterTmpl)
			t := template.New(
				filepath.Base(curSrc)).
				Funcs(map[string]any{"dict": Dict})

			if Config.IsEmbed {
				t, err = t.ParseFS(UrlFS, filterTmpl...)
			} else {
				t, err = t.ParseFiles(filterTmpl...)
			}

			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(err.Error()))
				return
			}
			_ = t.Execute(w, Config.Context)
		default:
			http.FileServer(http.Dir("."))
		}
	})
}

func main() {
	server := http.Server{Addr: "127.0.0.1:0", Handler: Mux}
	initURL()
	addr := server.Addr
	ln, _ := net.Listen("tcp", addr)
	fmt.Printf("http://127.0.0.1:%d\n", ln.Addr().(*net.TCPAddr).Port)
	_ = server.Serve(ln)
}
