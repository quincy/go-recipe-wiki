// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/russross/blackfriday"
)

// Page represents a single page in the wiki.
type Page struct {
	Title string
	Body  template.HTML
}

// save writes the page out to disk.
func (p *Page) save() error {
	filename := filepath.Join(pagesDir, p.Title+".txt")
	return ioutil.WriteFile(filename, []byte(p.Body), 0600)
}

// loadPage reads a page from disk.
func loadPage(title string) (*Page, error) {
	filename := filepath.Join(pagesDir, title+".txt")
	body, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return &Page{Title: title, Body: template.HTML(body)}, nil
}

// viewHandler prepares the page to be rendered by passing it through the
// markdown and wikiMarkup filters.
func viewHandler(w http.ResponseWriter, r *http.Request, title string) {
	p, err := loadPage(title)
	if err != nil {
		http.Redirect(w, r, "/edit/"+title, http.StatusFound)
		return
	}

	markdownBody := blackfriday.MarkdownCommon([]byte(p.Body))
	wikiMarkdown := convertWikiMarkup(markdownBody)
	p.Body = template.HTML(wikiMarkdown)
	renderTemplate(w, "view", p)
}

// editHandler loads an existing page from disk or creates a new empty page to
// be rendered.
func editHandler(w http.ResponseWriter, r *http.Request, title string) {
	p, err := loadPage(title)
	if err != nil {
		p = &Page{Title: title}
	}
	renderTemplate(w, "edit", p)
}

// saveHandler saves the changes and redirects back to the page's view.
func saveHandler(w http.ResponseWriter, r *http.Request, title string) {
	body := r.FormValue("body")
	p := &Page{Title: title, Body: template.HTML(body)}
	err := p.save()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/view/"+title, http.StatusFound)
}

// Parse the templates.
var templateDir string = "templates"
var templateFiles []string = []string{
	filepath.Join(templateDir, "edit.html"),
	filepath.Join(templateDir, "view.html")}

var templates = template.Must(template.ParseFiles(templateFiles...))

// renderTemplate takes the renders the html for the given template.
func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	err := templates.ExecuteTemplate(w, tmpl+".html", p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// Defines the set of valid URLs to expect.
var validPath = regexp.MustCompile("^/(edit|save|view)/([a-zA-Z0-9]+)$")

func makeHandler(fn func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := validPath.FindStringSubmatch(r.URL.Path)
		if m == nil {
			http.NotFound(w, r)
			return
		}
		fn(w, r, m[2])
	}
}

// a wikiLink looks like [[Words]]
var wikiLink = regexp.MustCompile("\\[\\[(Home)\\]\\]")

// convertWikiMarkup replaces wiki syntax with equivalent html.
func convertWikiMarkup(text []byte) []byte {
	var resultText = wikiLink.ReplaceAll(text, []byte("<a href=\"/view/$1\">$1</a>"))
	return resultText
}

// Ensure the pages directory exists before the program gets going.
var pagesDir string = "pages"

func init() {
	if _, err := os.Stat(pagesDir); os.IsNotExist(err) {
		if err := os.Mkdir(pagesDir, 0700); err != nil {
			panic(err)
		}
	}
}

func main() {
	var server = "localhost:8080"

	// open the default browser to the view/Home endpoint.
	var browser *exec.Cmd
	var url string = "http://" + server + "/view/Home"

	switch runtime.GOOS {
	case "windows":
		browser = exec.Command(`C:\Windows\System32\rundll32.exe`, "url.dll,FileProtocolHandler", url)
	case "darwin":
		browser = exec.Command("open", url)
	default:
		browser = exec.Command("xdg-open", url)
	}
	if err := browser.Start(); err != nil {
		panic(err)
	}

	// register the handlers and start the server.
	http.HandleFunc("/view/", makeHandler(viewHandler))
	http.HandleFunc("/edit/", makeHandler(editHandler))
	http.HandleFunc("/save/", makeHandler(saveHandler))
	http.Handle("/resources/", http.StripPrefix("/resources/", http.FileServer(http.Dir("resources"))))
	http.ListenAndServe(server, nil)
}
