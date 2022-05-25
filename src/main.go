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

var reTmpl *regexp.Regexp

func init() {
	reTmpl = regexp.MustCompile(`{{-? ?template \"(?P<Name>[^() ]*)\" ?.* ?-?}}`)
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

// getAllTmplName Get all template names, including nest.
func getAllTmplName(filePath string, allTmpl []string, isEmbed bool) (filterTmpl []string, err error) {
	var content []byte
	if isEmbed {
		content, err = UrlFS.ReadFile(filePath)
	} else {
		content, err = os.ReadFile(filePath)
	}
	if err != nil {
		return
	}
	matchList := reTmpl.FindAllStringSubmatch(string(content), -1)

	if len(matchList) == 0 {
		return
	}

	curTmplSet := map[string]string{} // Know the names of all the templates used in the current file
	for _, match := range matchList {
		tmplName := match[1]
		if _, exists := curTmplSet[tmplName]; exists {
			continue
		}
		curTmplSet[tmplName] = tmplName
	}

	for _, tmplFilepath := range allTmpl { // Select the all used template from allTmpl.
		_, exists := curTmplSet[filepath.Base(tmplFilepath)]
		if exists {
			filterTmpl = append(filterTmpl, tmplFilepath)
			fList, _ := getAllTmplName(tmplFilepath, allTmpl, isEmbed) // The template may also have a template (sub-template) again, so look for it again.
			if len(fList) > 0 {
				filterTmpl = append(filterTmpl, fList...)
			}
		}
	}
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
			filterTmpl, err := getAllTmplName(curSrc, tmplFileList, Config.IsEmbed)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			filterTmpl = append(filterTmpl, curSrc) // Must include self
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
