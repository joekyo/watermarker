# watermarker

Watermarker is a simple web service for adding watermark to your pictures.

To use this service, you need to upload two files, a png watermark file, and a zip file contains the pictures.
The server will return a zip file, which contains pictures that have watermark added.

To run this program, you need to install `ffmpeg` on your server first, then
run `go run watermarker.go` and open http://[SERVER-IP]:8848 in your browser.
