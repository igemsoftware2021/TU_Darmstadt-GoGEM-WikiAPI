package gogemwikiapi

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/publicsuffix"
)

// Login requests the loginURL, which should be a defined constant that leads to the first link in the iGEM login chain.
// Input : username, password, loginURL.
// Output: Pointer to Client with active session, if login was successful, otherwise an error will be returned.
func Login(username, password, loginUrl string) (*http.Client, error) {
	jar, err := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	if err != nil {
		log.Fatal(err)
	}

	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { // Redirects are not allowed, because this tends to mess with the cookie jar
			return http.ErrUseLastResponse
		}}

	req, err := http.NewRequest("POST", loginUrl, strings.NewReader(url.Values{"return_to": {""}, "username": {username}, "password": {password}, "Login": {"Login"}}.Encode())) // "Login" is the name of the submit button in the login form
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	final_url := ""

	for resp.StatusCode == 302 { // If we are redirected, we have to follow the redirect manually so we can gather all the cookies
		loc_url, err := resp.Location()
		if err != nil {
			log.Fatal(err)
		}
		resp, err = client.Get(strings.ReplaceAll(loc_url.String(), " ", "")) // Sometimes there are spaces in the URL, which causes problems, so we remove them
		if err != nil {
			log.Fatal(err)
		}
		final_url = loc_url.String()
	}

	if resp.StatusCode == 200 && strings.Contains(final_url, "Login_Confirmed") { // If we are not redirected further, and the last page url contains "Login_Confirmed" we are logged in
		return client, nil
	}

	return nil, errors.New("Login failed! Status: " + fmt.Sprint(resp.StatusCode)) // Ooops, something went wrong!
}

// Logout logs the user out of the iGEM website. logoutURL is the URL to the first page in the logout chain.
// Input: Pointer to Client, logoutURL.
// Output: No return value, but an error will be returned if something went wrong.
func Logout(client *http.Client, logoutUrl string) error {
	req, err := http.NewRequest("GET", logoutUrl, nil)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	final_url := ""

	for resp.StatusCode == 302 { // If we are redirected, we have to follow the redirect manually so we can gather all the cookies
		loc_url, err := resp.Location()
		if err != nil {
			log.Fatal(err)
		}
		resp, err = client.Get(strings.ReplaceAll(loc_url.String(), " ", "")) // Sometimes there are spaces in the URL, which causes problems, so we remove them
		if err != nil {
			log.Fatal(err)
		}
		final_url = loc_url.String()
	}
	if resp.StatusCode == 200 && strings.Contains(final_url, "Logout_Confirmed") { // If we are not redirected further, and the last page url contains "Logout_Confirmed" we are logged out
		return nil
	}
	return errors.New("Logout failed! Status: " + fmt.Sprint(resp.StatusCode)) // Ooops, something went wrong!
}

func Upload(client *http.Client, year int, teamname, pathtofile, offset string, file, force bool) string {

	var err error

	location := "" 
	filename := ""
	history_url := ""
	edit_url := "" 
	upload_url := "" 	

	if offset != "" {
		offset = offset + "/"
	}

	//Construct Filelocation relative to the Teamroot or iGEM "File:" Page, for pages and files respectively
	if file {
		filename := filepath.Base(pathtofile)
		location = "T--" + teamname + "--" + filename
		history_url, err = constructURL(year, teamname, location, file, false, false)
		if err != nil {
			log.Fatalln(err)
		}
		edit_url, err = constructURL(year, teamname, location, file, false, false)
		if err != nil {
			log.Fatalln(err)
		}
		upload_url, err = constructURL(year, teamname, location, file, true, false)
		if err != nil {
			log.Fatalln(err)
		}

	} 	else {
		filename := strings.Split(filepath.Base(pathtofile), ".")[0]
		location = offset + filename
		history_url, err = constructURL(year, teamname, location, file, false, true)
		if err != nil {
			log.Fatalln(err)
		}
		edit_url, err = constructURL(year, teamname, location, file, false, false)
		if err != nil {
			log.Fatalln(err)
		}
		upload_url, err = constructURL(year, teamname, location, file, true, false)
		if err != nil {
			log.Fatalln(err)
		}
	}

	//Generate Hash for the Object that will be uploaded
	fhash := gen_hash(pathtofile)

	//Check if the file already exists)
	already_uploaded, err := alreadyUploaded(client, history_url, fhash, file)
	if err != nil {
		log.Fatalln(err)
	}
	if already_uploaded && !force { // If the file is already uploaded and force is not set, we do not upload the file
		log.Fatalln("File already uploaded")
	}

	//Get the edit tokens
	payload := getTokens(client, edit_url)

	//Add the type specific data to the payload
	if file{
		fh, err := os.Open(pathtofile)
		if err != nil {
			log.Fatalln(err)
		}
		payload["wpUploadFile"] = fh
		payload["wpDestFile"] = strings.NewReader(filename)
		payload["wpUploadDescription"] = strings.NewReader("Hash:" + fhash)
		payload["wpIgnoreWarning"] = strings.NewReader("1")
	} else {
		fh , err := ioutil.ReadFile(pathtofile)
		if err != nil {
			log.Fatalln(err)
		}
		payload["wpTextbox1"] = strings.NewReader(string(fh))
		payload["wpSummary"] = strings.NewReader("Hash:" + fhash)
	}

	form, data_type := createMIMEMultipart(payload) // Create the multipart form for the upload

	req, err := http.NewRequest("POST", upload_url, form)
	if err != nil {
		log.Fatalln(err)
	}

	req.Header.Add("Content-Type", data_type)

	resp, err := client.Do(req) // Send the request
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	final_url := ""

	if resp.StatusCode != 302 { // If we are not redirected further, we have an error
		println("Upload did probably fail. Status: " + fmt.Sprint(resp.StatusCode) +"... Continuing")
	}

	for resp.StatusCode == 302 { // If we are redirected, we have to follow the redirect manually so we can gather all the cookies
		loc_url, err := resp.Location()
		if err != nil {
			log.Fatal(err)
		}
		resp, err = client.Get(strings.ReplaceAll(loc_url.String(), " ", "")) // Sometimes there are spaces in the URL, which causes problems, so we remove them
		if err != nil {
			log.Fatal(err)
		}
		final_url = loc_url.String()
	}
	
	return "Success! " + final_url

}

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