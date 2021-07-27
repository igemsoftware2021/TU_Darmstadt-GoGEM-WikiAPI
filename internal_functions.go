package gogemwikiapi

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Construct a valid URL for the iGEM website, depending on Filetype and if it should be viewed or uploaded.
func constructURL(year int, teamname, location string, file, upload, history bool) (string, error) {
	if file && upload{
		return "https://" + fmt.Sprint(year) + ".igem.org/Special:Upload", nil
	}
	if file && !upload{
		return "https://" + fmt.Sprint(year) + ".igem.org/File:" + location, nil
	}
	if !file && upload && !history{
		return "https://" + fmt.Sprint(year) + ".igem.org/Team:" + teamname + "/" + location + "?action=submit", nil
	}
	if !file && !upload && history{
		return "https://" + fmt.Sprint(year) + ".igem.org/Team:" + teamname + "/" + location + "?action=history", nil
	}
	if !file && !upload && !history{
		return "https://" + fmt.Sprint(year) + ".igem.org/Team:" + teamname + "/" + location + "?action=edit", nil
	}
	if !file && upload && history{
		return "", errors.New("cannot upload and view history at the same time")
	}
	return "", errors.New("unknown error") // Should never happen
}

// Generate a SHA256 hash for the file that will be uploaded.
func gen_hash(filepath string) string {
	h := sha256.New() // New Hasher

	file, err := os.Open(filepath) // Open File
	if err != nil {
		log.Fatalln(err)
	}
	defer file.Close() // Close when done

	if _, err := io.Copy(h, file); err != nil { // Copy file into hasher
		log.Fatalln(err)
	}

	return fmt.Sprintf("%x", h.Sum(nil)) // Return Hash

}

// Checks by comparing the file hash with the hash of the file on the iGEM website, if the file has changed.
func alreadyUploaded(client *http.Client, url, fhash string, file bool) (bool, error) {

	uploaded_hash := ""

	resp, err := client.Get(url) // Request the page with the object history
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode == 200 {
		body, err := goquery.NewDocumentFromReader(resp.Body) // Parse the HTML body with goquery
		if err != nil {
			log.Fatal(err)
		}
		if file{ // Differentiate between files and pages, file history is stored in a table
			sel := body.Find("tr") // Find all rows in the table
			if sel.Length() > 1 { // If there are more than one row, we have a file history
				uploaded_hash = sel.Slice(1,2).Find("td").Last().Text() // Get the content of the summary table entry for the last upload (the most recent one), if the file was uploaded with this tool, this is the file hash
			}
		} else {
			sel := body.Find("ul#pagehistory") // Find the history list
			if sel.Length() > 0 {
				uploaded_hash = sel.Find("li").First().Find("span[class=comment]").Text() // Get the commit comment of the first history entry, if the page was uploaded with this tool, this is the page hash
			}
		}
	}

	//Sanitize commit comment to get the hash (if there is one, otherwise its junk but that is ok, because wo only check if it is identical to the new hash)
	uploaded_hash = strings.ReplaceAll(uploaded_hash, "(", "") // Remove all opening parenthesis from the hash)
	uploaded_hash = strings.ReplaceAll(uploaded_hash, ")", "") // Remove all closing parenthesis from the hash)
	uploaded_hash = strings.ReplaceAll(uploaded_hash, " ", "") // Remove all spaces from the hash)
	helper := strings.Split(uploaded_hash, ":") // Split String at : into substrings
	uploaded_hash = helper[len(helper)-1] // Select last substring, which is the hash

	// println(uploaded_hash) // Print the hash for debugging purposes

	if uploaded_hash == fhash { // Compare the hashes
		return true, nil
	}

	return false, nil
}
// Gather editing tokens from the iGEM website. These get send as invisible inputs in the HTML form.
func getTokens(client *http.Client, edit_url string) map[string]io.Reader {
	payload := make(map[string]io.Reader) // Create a map to store the tokens
	resp, err := client.Get(edit_url)
	if err != nil {
		log.Fatalln(err)
	}
	defer resp.Body.Close()
	query, err := goquery.NewDocumentFromReader(resp.Body) // Parse the HTML
	if err != nil {
		log.Fatalln(err)
	}
	
	query.Find("input").Each(func(i int, s *goquery.Selection) { // Find all the input fields, some of them containing hidden values
		name := s.AttrOr("name", "none")
		value := s.AttrOr("value", "none")
		if name != "wpPreview" && name != "wpDiff"{ // wpPreview and wpDiff are responsible for showing the preview and diff respectively, which hinders our ability to upload the file
			payload[name] = strings.NewReader(value)
		}
	})

	return payload
}

// We need to construct a MIME Multpart Form to send the file to the iGEM website.
func createMIMEMultipart(payload map[string]io.Reader) (*bytes.Buffer, string) {
	var b bytes.Buffer // Buffer to store the form data
	w := multipart.NewWriter(&b) 
	for key, r := range payload { // Loop through the payload and write the form data
		var fw io.Writer
		var err error
		if x, ok := r.(io.Closer); ok { // If a file is given, we need to make sure it gets closed after we are done
			defer x.Close()
		}
		if x, ok := r.(*os.File); ok { // Check if the payload is a file, and if so create the right form entry
			if fw, err = w.CreateFormFile(key, x.Name()); err != nil {
				log.Fatalln(err)
			}
		} else {
			if fw, err = w.CreateFormField(key); err != nil { // If the payload is not a file, create a normal form entry
				log.Fatalln(err)
			}
		}
		if _, err := io.Copy(fw, r); err != nil { // Copy the payload into the form
			log.Fatalln(err)
		}
	}
	w.Close() // After we are done we need to close the form writer, so that the closing boundry is written to the buffer
	return &b, w.FormDataContentType() // Return the buffer and the content type. In the content type the boundry is defined
}