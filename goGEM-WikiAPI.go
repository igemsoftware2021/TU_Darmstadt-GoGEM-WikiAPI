package gogemwikiapi

import (
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/cookiejar"

	// "net/http/httputil" // DEBUG
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
		return nil, err
	}

	client := &http.Client{
		Jar: jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { // Redirects are not allowed, because this tends to mess with the cookie jar
			return http.ErrUseLastResponse
		}}

	req, err := http.NewRequest("POST", loginUrl, strings.NewReader(url.Values{"return_to": {""}, "username": {username}, "password": {password}, "Login": {"Login"}}.Encode())) // "Login" is the name of the submit button in the login form
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	final_url := ""

	for resp.StatusCode == 302 { // If we are redirected, we have to follow the redirect manually so we can gather all the cookies
		loc_url, err := resp.Location()
		if err != nil {
			return nil, err
		}
		resp, err = client.Get(strings.ReplaceAll(loc_url.String(), " ", "")) // Sometimes there are spaces in the URL, which causes problems, so we remove them
		if err != nil {
			return nil, err
		}
		final_url = loc_url.String()
	}

	if resp.StatusCode == 200 && strings.Contains(final_url, "Login_Confirmed") { // If we are not redirected further, and the last page url contains "Login_Confirmed" we are logged in
		return client, nil
	}

	return nil, errors.New("loginFailed") // Ooops, something went wrong!
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
	return errors.New("logoutFailed") // Ooops, something went wrong!
}

// Upload is the wrapper for the whole upload process.
// Input: Session, Wiki year, Teamname with spaces as underscore, filepath to the file to upload, offset, if it is a file(media files etc.), and if already uploaded files should be overwritten (already uploaded is determined by a hash compare).
// Output: String containing the URL to the uploaded page / the file overview page, or an error if something went wrong.
func Upload(client *http.Client, year int, teamname, pathToFile, offset string, file, force bool) (string,error) {

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
		filename = filepath.Base(pathToFile)
		location = "T--" + teamname + "--" + filename
		history_url, err = constructURL(year, teamname, location, file, false, false)
		if err != nil {
			return "", err
		}
		edit_url, err = constructURL(year, teamname, location, file, true, false)
		if err != nil {
			return "", err
		}
		upload_url, err = constructURL(year, teamname, location, file, true, false)
		if err != nil {
			return "", err
		}

	} 	else {
		filename := strings.Split(filepath.Base(pathToFile), ".")[0]
		if strings.Contains(strings.Split(filepath.Base(pathToFile), ".")[1], "min") {
			filename = filename + "-min"
		}
		if !strings.Contains(filename, "index") {
			location = offset + filename
		} else {
			location = offset
		}
		history_url, err = constructURL(year, teamname, location, file, false, true)
		if err != nil {
			return "", err
		}
		edit_url, err = constructURL(year, teamname, location, file, false, false)
		if err != nil {
			return "", err
		}
		upload_url, err = constructURL(year, teamname, location, file, true, false)
		if err != nil {
			return "", err
		}
		//DEBUG:
		// println(filename)
		// println(location)
	}

	// DEBUG:
	// println(history_url)
	// println(edit_url)
	// println(upload_url)
	// println(pathToFile)
	// panic(err)

	//Generate Hash for the Object that will be uploaded
	fhash := gen_hash(pathToFile)

	//Check if the file already exists)
	already_uploaded, err := alreadyUploaded(client, history_url, fhash, file)
	if err != nil {
		return "", err
	}
	if already_uploaded && !force { // If the file is already uploaded and force is not set, we do not upload the file
		return history_url, errors.New("fileAlreadyUploaded")
	}

	//Get the edit tokens
	payload := getTokens(client, edit_url)

	// DEBUG:
	// for key, value := range payload {
	// 	println(key, value)
	// }
	// panic(err)

	//Add the type specific data to the payload 
	if file{
		fh, err := os.Open(pathToFile)
		if err != nil {
			return "", err
		}
		payload["wpUploadFile"] = fh
		payload["wpDestFile"] = strings.NewReader(location)
		payload["wpUploadDescription"] = strings.NewReader("Hash:" + fhash)
		payload["wpIgnoreWarning"] = strings.NewReader("1")
	} else {
		fh , err := ioutil.ReadFile(pathToFile)
		if err != nil {
			return "", err
		}
		payload["wpTextbox1"] = strings.NewReader(string(fh))
		payload["wpSummary"] = strings.NewReader("Hash:" + fhash)
	}

	form, data_type := createMIMEMultipart(payload) // Create the multipart form for the upload

	req, err := http.NewRequest("POST", upload_url, form)
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", data_type)

	// DEBUG: 
	// reqDump, err := httputil.DumpRequest(req, true)
	// println(string(reqDump))
	// panic(err)

	resp, err := client.Do(req) // Send the request
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	final_url := ""

	if resp.StatusCode != 302 { // If we are not redirected further, we have an error
		// println("Upload did probably fail. Status: " + fmt.Sprint(resp.StatusCode) +"... Continuing")
		return "", errors.New("uploadDidFail")
	}

	for resp.StatusCode == 302 { // If we are redirected, we have to follow the redirect manually so we can gather all the cookies
		loc_url, err := resp.Location()
		if err != nil {
			return "", err
		}
		resp, err = client.Get(strings.ReplaceAll(loc_url.String(), " ", "")) // Sometimes there are spaces in the URL, which causes problems, so we remove them
		if err != nil {
			return "", err
		}
		final_url = loc_url.String()
	}
	println(final_url)
	return final_url, nil

}

func GetFileUrl(file_overview_url string, client *http.Client) (string, error){

	ret_url := ""

	resp, err := client.Get(file_overview_url)
	if err != nil || resp.StatusCode != 200 {
		return "", err
	}
	defer resp.Body.Close()

	// DEBUG:
	// dumpBody,_ := httputil.DumpResponse(resp, true)
	// println(string(dumpBody))

	body, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	body.Find("div[class=fullMedia]").Each(func(i int, s *goquery.Selection) {
		ret_url = s.Find("a").AttrOr("href", "")
	})

	return ret_url, nil
}

func Debug(year int, teamname string){

	getAllPages(year, teamname)

}