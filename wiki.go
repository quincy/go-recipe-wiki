// Copyright 2010 The Go Authors.
// Copyright 2014 Quincy Bowers.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/russross/blackfriday"
)

// Page represents a single page in the wiki.
type Page struct {
	Title        string
	Ingredients  template.HTML
	Instructions template.HTML
	Index        []string
}

type RootPage struct {
	Title string
	Body  template.HTML
	Index []string
}

// save writes the page out to disk.
func (p *Page) save() error {
	body := fmt.Sprintf("<!-- Ingredients -->\n%s\n<!-- Instructions -->\n%s", p.Ingredients, p.Instructions)
	filename := filepath.Join(pagesDir, p.Title+".txt")
	return ioutil.WriteFile(filename, []byte(body), 0600)
}

// loadPage reads a page from disk.
func loadPage(title string) (*Page, error) {
	filename := filepath.Join(pagesDir, title+".txt")
	body, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	ingredients, instructions := parseRecipe(body)
	return &Page{Title: title, Ingredients: template.HTML(ingredients), Instructions: template.HTML(instructions)}, nil
}

func loadRoot(title string) (*RootPage, error) {
	filename := filepath.Join(pagesDir, title+".txt")
	body, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	return &RootPage{Title: title, Body: template.HTML(body), Index: pages}, nil
}

// rootHandler prepares the home page.
func rootHandler(w http.ResponseWriter, r *http.Request, title string) {
	p, err := loadRoot(title)

	p.Body = template.HTML(blackfriday.MarkdownCommon([]byte(p.Body)))
	p.Body = template.HTML(convertWikiMarkup([]byte(p.Body)))

	err = templates.ExecuteTemplate(w, "root.html", p)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// viewHandler prepares the page to be rendered by passing it through the
// markdown and wikiMarkup filters.
func viewHandler(w http.ResponseWriter, r *http.Request, title string) {
	// Special case for the root page.
	if title == rootTitle {
		rootHandler(w, r, title)
		return
	}

	p, err := loadPage(title)
	if err != nil {
		http.Redirect(w, r, "/edit/"+title, http.StatusFound)
		return
	}

	p.Ingredients = template.HTML(blackfriday.MarkdownCommon([]byte(p.Ingredients)))
	p.Instructions = template.HTML(blackfriday.MarkdownCommon([]byte(p.Instructions)))
	p.Ingredients = template.HTML(convertWikiMarkup([]byte(p.Ingredients)))
	p.Instructions = template.HTML(convertWikiMarkup([]byte(p.Instructions)))
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
	ingredients := r.FormValue("ingredients")
	instructions := r.FormValue("instructions")
	recipeTitle := r.FormValue("recipeTitle")

	// TODO If the recipeTitle is different than the title we are renaming and
	//      need to delete the old title.txt.

	p := &Page{Title: recipeTitle, Ingredients: template.HTML(ingredients), Instructions: template.HTML(instructions)}
	err := p.save()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	updateIndex()
	http.Redirect(w, r, "/view/"+recipeTitle, http.StatusFound)
}

// Parse the templates.
var templateDir string = "templates"
var templateFiles []string = []string{
	filepath.Join(templateDir, "root.html"),
	filepath.Join(templateDir, "edit.html"),
	filepath.Join(templateDir, "view.html")}

var templates = template.Must(template.ParseFiles(templateFiles...))

// renderTemplate takes the renders the html for the given template.
func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	p.Index = pages

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
var wikiLink = regexp.MustCompile("\\[\\[([a-zA-Z0-9]+)\\]\\]")

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

// get a list of all of the pages
type Pages []string

func (p Pages) Len() int {
	return len(p)
}

func (p Pages) Less(i, j int) bool {
	if p[i] == "Home" {
		return true
	} else if p[j] == "Home" {
		return false
	}
	return p[i] < p[j]
}

func (p Pages) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
}

var pages Pages

// Get an initial list of all of the pages.
func init() {
	updateIndex()
	sort.Sort(pages)
}

// updateIndex reads the list of files in pages/ and creates a sorted index.
// The Home page sorts ahead of all others.
func updateIndex() {
	dirs, err := ioutil.ReadDir(pagesDir)
	if err != nil {
		panic(err)
	}

	pages = make([]string, 0)

	for _, v := range dirs {
		if !strings.HasPrefix(v.Name(), ".") {
			pages = append(pages, strings.Replace(v.Name(), ".txt", "", -1))
		}
	}
}

// parseRecipe separates the loaded page into ingredients and instructions.
func parseRecipe(content []byte) (ingredients, instructions template.HTML) {
	lines := strings.Split(string(content), "\n")

	inIngredients := false
	inInstructions := false

	for _, line := range lines {
		switch line {
		case "<!-- Ingredients -->":
			inIngredients = true
			inInstructions = false
		case "<!-- Instructions -->":
			inIngredients = false
			inInstructions = true
		default:
			if inIngredients {
				ingredients += template.HTML(line + "\n")
			} else if inInstructions {
				instructions += template.HTML(line + "\n")
			} else {
				panic(errors.New("Found bad line!  " + line))
			}
		}
	}

	return
}

var rootTitle string = "Home"

func main() {
	var server = "localhost:8080"

	// open the default browser to the view/Home endpoint.
	var browser *exec.Cmd
	var url string = "http://" + server + "/view/" + rootTitle

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
