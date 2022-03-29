package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

type RequestData struct {
	Branch  string            `json:"branch"`
	Modules map[string]string `json:"modules"`
}

var dir = flag.String("d", "./", "directory to watch")
var email = flag.String("e", "", "user email")
var password = flag.String("p", "", "user password")

func main() {
	flag.Parse()

	if *email == "" || *password == "" {
		log.Fatal("email and password required")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()

	needsUpdate := int32(1)
	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Write == fsnotify.Write {
					if filepath.Ext(event.Name) != ".js" {
						continue
					}

					atomic.StoreInt32(&needsUpdate, 1)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println(err)
			case <-done:
				return
			}
		}
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	go func() {
		for {
			select {
			case <-ticker.C:
				if atomic.CompareAndSwapInt32(&needsUpdate, 1, 0) {
					log.Printf("uploading modules...")
					err := uploadCode(*email, *password, *dir)
					if err != nil {
						log.Println("could not upload modules:", err)
					} else {
						log.Println("modules uploaded")

					}
				}
			case <-done:
				return
			}
		}
	}()

	err = watcher.Add(*dir)
	if err != nil {
		log.Fatal(err)
	}
	<-done
}

func uploadCode(email, password, dir string) error {
	modules, err := gatherModules(dir)
	if err != nil || len(modules) == 0 {
		return err
	}

	d := RequestData{
		Branch:  "default",
		Modules: modules,
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}

	r := bytes.NewReader(b)
	req, err := http.NewRequest("POST", "https://screeps.com/api/user/code", r)
	if err != nil {
		return err
	}

	authStr := base64.StdEncoding.EncodeToString([]byte(email + ":" + password))
	req.Header.Set("Authorization", "Basic "+authStr)
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return fmt.Errorf("could not upload the code: %d", res.StatusCode)
	}

	return nil
}

func gatherModules(dir string) (map[string]string, error) {
	modules := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		ext := filepath.Ext(path)
		if d.IsDir() || ext != ".js" {
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}

		b, err := io.ReadAll(f)
		if err != nil {
			return err
		}

		modName := strings.ReplaceAll(path, ext, "")
		modules[modName] = string(b)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return modules, nil
}
