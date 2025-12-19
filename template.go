package main

import (
	"fmt"
	"html/template"
	"log"
	"os"
	"path"
	"sync"
	"time"
)

type TemplateCacheEntry struct {
	T        *template.Template
	fileInfo os.FileInfo
}

var (
	TemplateCache      = map[string]*TemplateCacheEntry{}
	TemplateCacheMutex sync.Mutex
)

func templateFuncBitAnd(x1 uint32, x2 uint32) uint32 {
	return x1 & x2
}

func templateFuncLoop(n int) []struct{} {
	return make([]struct{}, n)
}

func templateFmtTime(t time.Time, format string) string {
	return t.Format(format)
}

func templateFmtAge(t time.Time) string {
	var d = time.Now().Sub(t)
	logger.Debugf("d = %v", d)
	if d.Hours() > 24.0 {
		x := int64(d.Seconds())
		days := x / 86400
		x = x % 86400
		h := x / 3600
		x = x % 3600
		m := x / 60
		s := x % 60
		return fmt.Sprintf("%dd %02d:%02d:%02d", days, h, m, s)
	} else {
		x := int64(d.Seconds())
		h := x / 3600
		x = x % 3600
		m := x / 60
		s := x % 60
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
}

func getTemplate(name string, additionalParts ...string) (*template.Template, error) {
	var (
		t   *TemplateCacheEntry
		err error
	)

	parts := make([]string, 3+len(additionalParts))
	parts[0] = path.Join(Config.Web.TemplatePath, name)
	parts[1] = path.Join(Config.Web.TemplatePath, "header.tmpl")
	parts[2] = path.Join(Config.Web.TemplatePath, "footer.tmpl")
	for i, p := range additionalParts {
		parts[i+3] = path.Join(Config.Web.TemplatePath, p)
	}
	templateFuncs := template.FuncMap{
		"bitand":  templateFuncBitAnd,
		"loop":    templateFuncLoop,
		"fmttime": templateFmtTime,
		"fmtage":  templateFmtAge,
	}
	TemplateCacheMutex.Lock()
	defer TemplateCacheMutex.Unlock()
	if t, ok := TemplateCache[parts[0]]; ok {
		// found template in cache, see if we need to update it
		reparse := false
		for _, p := range parts {
			if f, err := os.Stat(p); err == nil {
				if f.ModTime().After(t.fileInfo.ModTime()) {
					reparse = true
					log.Printf("Out-of-date template part %s found, reparsing\n", p)
					break
				}
			}
		}
		if reparse {
			newT := template.New(name).Funcs(templateFuncs)
			if newT, err = newT.ParseFiles(parts...); err != nil {
				log.Println("Error reparsing:", err)
				return nil, err
			} else {
				t.T = newT
			}
		}
		return t.T, nil
	}
	t = new(TemplateCacheEntry)
	for i, p := range parts {
		if f, err := os.Stat(p); err == nil {
			if i == 0 || f.ModTime().After(t.fileInfo.ModTime()) {
				t.fileInfo = f
			}
		}
	}
	t.T = template.New(name).Funcs(templateFuncs)
	if t.T, err = t.T.ParseFiles(parts...); err != nil {
		return nil, err
	}
	TemplateCache[parts[0]] = t
	return t.T, nil
}
