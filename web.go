package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/djavorszky/ddn-api/database/data"
	"github.com/djavorszky/ddn-api/registry"
	"github.com/djavorszky/ddn-common/brwsr"
	"github.com/djavorszky/ddn-common/logger"
	"github.com/djavorszky/ddn-common/model"
	"github.com/djavorszky/liferay"
	"github.com/gorilla/mux"
)

// Page is a struct holding the data to be displayed on the welcome page.
type Page struct {
	UseCDN                 bool
	Agents                 []model.Agent
	AnyOnline              bool
	Title                  string
	Pages                  map[string]string
	ActivePage             string
	Message                string
	MessageType            string
	User                   string
	HasUser                bool
	UserMD5                string
	HasEntry               bool
	PrivateDatabases       []data.Row
	PublicDatabases        []data.Row
	HasPrivateDBs          bool
	HasPublicDBs           bool
	Ext62                  liferay.JDBC
	ExtDXP                 liferay.JDBC
	FileList               brwsr.FileList
	HasMountedFolder       bool
	WebPushEnabled         bool
	DumpLoc                string
	Version                string
	BuildTime              string
	Commit                 string
	GoogleAnalyticsEnabled bool
	GoogleAnalyticsID      string
}

// GetMD5Hash gives back the MD5 checksum of a string
// Source: https://gist.github.com/sergiotapia/8263278
func GetMD5Hash(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}

func loadPage(w http.ResponseWriter, r *http.Request, pages ...string) {
	page := Page{
		Agents:                 registry.List(),
		Title:                  getTitle(r.URL.Path),
		Pages:                  getPages(),
		ActivePage:             r.URL.Path,
		HasMountedFolder:       config.MountLoc != "",
		WebPushEnabled:         config.WebPushEnabled,
		Version:                version,
		BuildTime:              buildTime,
		Commit:                 commit,
		GoogleAnalyticsEnabled: config.GoogleAnalyticsID != "",
		GoogleAnalyticsID:      config.GoogleAnalyticsID,
	}

	for _, agent := range registry.List() {
		if agent.Up {
			page.AnyOnline = true
			break
		}
	}

	userCookie, err := r.Cookie("user")
	if err != nil || userCookie.Value == "" {
		// if there's an err, it can only happen if there is no cookie.
		toLoad := []string{"base", "nav", "login"}
		tmpl, err := buildTemplate(toLoad...)
		if err != nil {
			panic(err)
		}

		err = tmpl.ExecuteTemplate(w, "base", page)
		if err != nil {
			panic(err)
		}
		return
	}

	page.User = userCookie.Value
	page.HasUser = true
	page.UserMD5 = GetMD5Hash(userCookie.Value) // email MD5

	session, err := store.Get(r, "user-session")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	if flashes := session.Flashes("success"); len(flashes) > 0 {
		page.Message = flashes[0].(string)
		page.MessageType = "success"

		id, ok := session.Values["id"].(int)
		if !ok {
			session.Values["id"] = 0
		}

		if id != 0 {
			page.HasEntry = true
			entry, err := db.FetchByID(id)
			if err != nil {
				logger.Error("database query: %v", err)
				session.AddFlash("Failed querying database", "fail")
			} else {
				switch entry.DBVendor {
				case "mysql":
					page.Ext62 = liferay.MysqlJDBC(entry.DBAddress, entry.DBName, entry.DBUser, entry.DBPass)
					page.ExtDXP = liferay.MysqlJDBCDXP(entry.DBAddress, entry.DBName, entry.DBUser, entry.DBPass)
				case "mariadb":
					page.Ext62 = liferay.MariaDBJDBC(entry.DBAddress, entry.DBName, entry.DBUser, entry.DBPass)
					page.ExtDXP = page.Ext62
				case "postgres":
					page.Ext62 = liferay.PostgreJDBC(entry.DBAddress, entry.DBName, entry.DBUser, entry.DBPass)
					page.ExtDXP = page.Ext62
				case "oracle":
					page.Ext62 = liferay.OracleJDBC(entry.DBAddress, entry.DBSID, entry.DBUser, entry.DBPass)
					page.ExtDXP = page.Ext62
				case "mssql":
					page.Ext62 = liferay.MSSQLJDBC(entry.DBAddress, entry.DBName, entry.DBUser, entry.DBPass)
					page.ExtDXP = liferay.MSSQLJDBCDXP(entry.DBAddress, entry.DBName, entry.DBUser, entry.DBPass)
				}
			}
		}
	} else if flashes := session.Flashes("fail"); len(flashes) > 0 {
		page.Message = flashes[0].(string)
		page.MessageType = "danger"
	} else if flashes := session.Flashes("msg"); len(flashes) > 0 {
		page.Message = flashes[0].(string)
		page.MessageType = "success"
	} else {
		page.Message = ""
	}

	/*
		// DEBUG:
		if !page.HasEntry {
			page.HasEntry = true
			entry := db.FetchByID(1)

			page.ExtDXP = portalExt(entry, true)
			page.Ext62 = portalExt(entry, false)
		}
	*/
	session.Save(r, w)

	if pages[0] == "browse" && page.HasMountedFolder {
		loc := mux.Vars(r)["loc"]

		files, err := brwsr.List(loc)
		if err != nil {
			session.AddFlash("Failed listing folder", "fail")
		}

		page.FileList = files
	}

	if pages[0] == "srvimport" {
		dumploc := r.URL.Query().Get("dump")

		page.DumpLoc = dumploc
	}

	if pages[0] == "home" {
		pages = append(pages, "databases")

		privateDBs, err := db.FetchByCreator(page.User)
		if err != nil {
			logger.Error("couldn't list databases: %v", err)
		}

		if len(privateDBs) != 0 {
			page.PrivateDatabases = privateDBs
			page.HasPrivateDBs = true
		}

		publicDBs, err := db.FetchPublic()
		if err != nil {
			logger.Error("couldn't list databases: %v", err)
		}

		if len(publicDBs) != 0 {
			page.PublicDatabases = publicDBs
			page.HasPublicDBs = true
		}
	}

	toLoad := []string{"base", "nav", "properties"}
	toLoad = append(toLoad, pages...)

	tmpl, err := buildTemplate(toLoad...)
	if err != nil {
		panic(err)
	}

	err = tmpl.ExecuteTemplate(w, "base", page)
	if err != nil {
		panic(err)
	}
}

func buildTemplate(pages ...string) (*template.Template, error) {
	var templates []string
	for _, page := range pages {
		templates = append(templates, fmt.Sprintf("%s/web/html/%s.html", workdir, page))
	}

	tmpl, err := template.ParseFiles(templates...)
	if err != nil {
		return nil, fmt.Errorf("parsing templates failed: %s", err.Error())
	}

	return tmpl, nil
}

func getTitle(page string) string {
	title, ok := getPages()[page]
	if ok {
		return title
	}

	if strings.HasPrefix(page, "/browse") {
		return "Server Browser"
	}

	switch page {
	case "/fileimport", "/srvimport":
		return "Import Database"
	default:
		return "Unknown"
	}

}

func getPages() map[string]string {
	pages := make(map[string]string)

	pages["/"] = "Home"
	pages["/createdb"] = "Create database"
	pages["/importdb"] = "Import database"

	return pages
}
