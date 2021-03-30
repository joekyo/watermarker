package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// TODO: limit upload file size

var debug = false

func init() {
	if len(os.Args) == 2 && os.Args[1] == "-debug" {
		debug = true
	}
}

func dbg(format string, a ...interface{}) {
	if debug {
		log.Printf(format, a...)
	}
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		dbg("Got HTTP request from %s %s %s", r.RemoteAddr, r.Method, r.URL.Path)
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, form)
		} else {
			watermark(w, r)
		}
	})
	dbg("Start HTTP server on :8848")
	log.Fatal(http.ListenAndServe(":8848", nil))
}

func watermark(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1024); err != nil {
		dbg("Error bad request from %s", r.RemoteAddr)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	tdir, err := ioutil.TempDir("", "watermark_")
	if err != nil {
		dbg("Error create temp dir %s: %v", tdir, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if debug {
		// keep temp dir in debug mode
		dbg("Created temp dir %s", tdir)
	} else {
		defer os.RemoveAll(tdir)
	}

	_, pngPath, ok := dumpFormFile(r.MultipartForm.File, tdir, "png")
	if !ok {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	zipName, zipPath, ok := dumpFormFile(r.MultipartForm.File, tdir, "zip")
	if !ok {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := rezip(zipPath, pngPath, tdir, zw); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	zw.Close() // must close zip writer before write response; cannot use defer here

	// ready to write response
	newZipName := strings.Replace(zipName, ".zip", ".watermark.zip", 1)
	w.Header().Set("Content-Disposition", `attachment; filename="`+newZipName+`"`)
	dbg("Write response with file %s", newZipName)
	io.Copy(w, &buf)
}

// rezip reads uploaded zip file, creates new zip and writes to zw
func rezip(zipPath, pngPath, tdir string, zw *zip.Writer) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	os.MkdirAll(filepath.Join(tdir, "out"), 0755)
	dbg("Created dir %s", filepath.Join(tdir, "out"))

	for _, f := range zr.File {
		name := f.Name

		// create header for writer
		hdr := &zip.FileHeader{
			Name:   name,
			Method: zip.Store, // no compression
		}

		in, err := f.Open()
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := zw.CreateHeader(hdr) // out is Writer, no need to close it
		if err != nil {
			return err
		}

		switch {
		case f.FileInfo().IsDir():
			err = createDirs(tdir, name)
		case isMediaFile(name):
			err = addWatermark(pngPath, tdir, name, in, out)
		default:
			_, err = io.Copy(out, in) // for non media files, just copy them
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func isMediaFile(name string) bool {
	for _, s := range []string{".bmp", ".jpg", ".png", ".mp4"} {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

func createDirs(dir, subDir string) error {
	p1 := filepath.Join(dir, subDir)
	p2 := filepath.Join(dir, "out", subDir)
	for _, p := range []string{p1, p2} {
		if err := os.MkdirAll(p, 0755); err != nil {
			dbg("Error create dir %s: %v", p, err)
			return err
		}
		dbg("Created dir %s", p)
	}
	return nil
}

// addWatermark reads input from media files, adds watermark, saves result to tdir/name and out
func addWatermark(watermark, tdir, name string, in io.ReadCloser, out io.Writer) (err error) {
	defer in.Close()

	input := filepath.Join(tdir, name)
	output := filepath.Join(tdir, "out", name)

	p, err := ioutil.ReadAll(in)
	if err != nil {
		dbg("Error read file %s: %v", input, err)
		return
	}
	if err = ioutil.WriteFile(input, p, 0644); err != nil {
		dbg("Error write file %s: %v", input, err)
		return
	}

	dbg("Run ffmpeg %s %s", input, output)
	args := []string{"-i", input, "-i", watermark, "-filter_complex", "overlay=main_w-overlay_w-10:10", "-codec:a", "copy", output}
	if err = exec.Command("/usr/local/bin/ffmpeg", args...).Run(); err != nil {
		dbg("Error ffmpeg %s %s: %v", input, output, err)
		return
	}

	p, err = ioutil.ReadFile(output)
	if err != nil {
		dbg("Error read file %s: %v", output, err)
		return
	}
	if _, err = out.Write(p); err != nil {
		dbg("Error write zip file: %v", err)
		return
	}
	return
}

// dumpFormFile returns filename in form, file path saved on file system on success
func dumpFormFile(hdr map[string][]*multipart.FileHeader, dir, id string) (name string, path string, ok bool) {
	if len(hdr[id]) == 0 {
		return
	}
	f := hdr[id][0]
	name = f.Filename
	path = filepath.Join(dir, name)

	input, err := f.Open()
	if err != nil {
		dbg("Error open zip file: %v", err)
		return
	}
	p, err := ioutil.ReadAll(input)
	if err != nil {
		dbg("Error read zip file: %v", err)
		return
	}
	if err = ioutil.WriteFile(path, p, 0755); err != nil {
		dbg("Error write %s: %v", path, err)
		return
	}
	dbg("Saved form file %s to %s", name, path)
	return name, path, true
}

const form = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>add watermark</title>
  <style>
    label, input, button {
      margin: 5px auto;
      cursor: pointer;
    }
  </style>
</head>
<body>
  <form action="/" method="post" enctype="multipart/form-data">
    <label for="zip">upload zip</label>
    <input type="file" accept=".zip" id="zip" name="zip" required /><br>
    <label for="png">upload png</label>
    <input type="file" accept="image/png" id="png" name="png" required /><br>
    <button type="submit">go</button>
  </form>
  <script>
  </script>
</body>
</html>
`
