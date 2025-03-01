package cmd

import (
	"fmt"
	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/google/uuid"
	"html/template"
	"io/fs"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sensepost/gowitness/lib"
	"github.com/sensepost/gowitness/storage"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var (
	rsDB  *gorm.DB
	theme string = "dark" // or light
)

// serverCmd represents the server command
var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Starts a webserver that serves the report interface, api and screenshot tool",
	Long: `Starts a webserver that serves the report interface, api and screenshot tool.

The report server is availabe in the root path, aka /.
The API is available from the /api path.

The global database and screenshot paths should be set to the same as
what they were when a scan was run. The report server also has the ability
to screenshot ad-hoc URLs provided to the submission page.

The API is usable to take screenshots and reflect them back amongst other useful things.
Most of the Gowitness core is exposed via the API.

NOTE: When changing the server address to something other than localhost, make 
sure that only authorised connections can be made to the server port. By default,
access is restricted to localhost to reduce the risk of SSRF attacks against the
host or hosting infrastructure (AWS/Azure/GCP, etc). Consider strict IP filtering
or fronting this server with an authentication aware reverse proxy.

Allowed URLs, by default, need to start with http:// or https://. If you need
this restriction lifted, add the --allow-insecure-uri / -A flag. A word of 
warning though, that also means that someone may request a URL like file:///etc/passwd.
`,
	Example: `$ gowitness server
$ gowitness server --address 0.0.0.0:8080
$ gowitness server --address 127.0.0.1:9000 --allow-insecure-uri`,
	Run: func(cmd *cobra.Command, args []string) {
		log := options.Logger

		if !strings.Contains(options.ServerAddr, "localhost") {
			log.Warn().Msg("exposing this server to other networks is dangerous! see the server command help for more information")
		}
		if db.Platform == storage.Sqlite {
			if !strings.HasPrefix(options.BasePath, "/") {
				log.Warn().Msg("base path does not start with a /")
			}
		}

		if db.Platform == storage.Postgres {
			if !strings.HasPrefix(options.BasePath, "postgresql") {
				log.Warn().Msg("base path does not start with a /")
			}

		}

		// db
		dbh, err := db.Get()
		if err != nil {
			log.Fatal().Err(err).Msg("could not gt db handle")
		}
		rsDB = dbh

		log.Info().Str("path", db.Path).Msg("db path")
		log.Info().Str("path", options.ScreenshotPath).Msg("screenshot path")

		if options.Debug {
			gin.SetMode(gin.DebugMode)
		} else {
			gin.SetMode(gin.ReleaseMode)
		}

		// To initialize Sentry's handler, you need to initialize Sentry itself beforehand
		if err := sentry.Init(sentry.ClientOptions{
			Dsn:           os.Getenv("SENTRY_DSN"),
			EnableTracing: false,
			Environment:   os.Getenv("SENTRY_ENV"),
			// Set TracesSampleRate to 1.0 to capture 100%
			// of transactions for performance monitoring.
			// We recommend adjusting this value in production,
			TracesSampleRate: 0,
		}); err != nil {
			log.Warn().Msgf("Sentry initialization failed: %v\n", err)
		}

		r := gin.Default()
		r.Use(themeChooser(&theme))
		r.Use(sentrygin.New(sentrygin.Options{
			Repanic: true,
		}))

		// add / suffix to the base url so that we can be certain about
		// the trim in the template helper
		if !strings.HasSuffix(options.BasePath, "/") {
			options.BasePath += "/"
		}

		log.Info().Str("base-path", options.BasePath).Msg("basepath")

		funcMap := template.FuncMap{
			"GetTheme": getTheme,
			"Contains": func(full string, search string) bool {
				return strings.Contains(full, search)
			},
			"URL": func(url string) string {
				return options.BasePath + strings.TrimPrefix(url, "/")
			},
		}
		tmpl := template.Must(template.New("").Funcs(funcMap).ParseFS(Embedded, "web/ui-templates/*.html"))
		r.SetHTMLTemplate(tmpl)

		// web ui routes
		r.GET("/", dashboardHandler)
		r.GET("/gallery", galleryHandler)
		r.GET("/table", tableHandler)
		r.GET("/details/:id", detailHandler)
		r.GET("/details/:id/dom", detailDOMDownloadHandler)
		r.GET("/submit", getSubmitHandler)
		r.POST("/submit", submitHandler)
		r.POST("/search", searchHandler)

		// static assets & raw screenshot files
		assetFs, err := fs.Sub(Embedded, "web/assets")
		if err != nil {
			log.Fatal().Err(err).Msg("could not fs.Sub Assets")
		}

		// assets & screenshots
		r.StaticFS("/assets/", http.FS(assetFs))
		r.StaticFS("/screenshots", http.Dir(options.ScreenshotPath))

		// json api routes
		api := r.Group("/api")
		{
			api.GET("/list", apiURLHandler)
			api.GET("/search", apiSearchHandler)
			api.GET("/detail/:id", apiDetailHandler)
			api.GET("/status/:uuid", apiStatusHandler)
			api.GET("/detail/:id/screenshot", apiDetailScreenshotHandler)
			api.POST("/screenshot", apiScreenshotHandler)
		}

		log.Info().Str("address", options.ServerAddr).Msg("server listening")
		if err := r.Run(options.ServerAddr); err != nil {
			log.Fatal().Err(err).Msg("webserver failed")
		}
	},
}

func init() {
	rootCmd.AddCommand(serverCmd)

	serverCmd.Flags().StringVarP(&options.ServerAddr, "address", "a", "localhost:7171", "server listening address")
	serverCmd.Flags().BoolVarP(&options.AllowInsecureURIs, "allow-insecure-uri", "A", false, "allow uris that dont start with http(s)")
	serverCmd.Flags().StringVarP(&options.BasePath, "base-path", "b", "/", "set the servers base path (useful for some reverse proxy setups)")
}

// middleware
// --

// getTheme gets the current theme choice
func getTheme() string {
	return theme
}

// themeChooser is a middleware to set the theme to use in the base template
func themeChooser(choice *string) gin.HandlerFunc {
	return func(c *gin.Context) {

		// parse the query string as preference. this will indicate a theme switch
		q := c.Query("theme")
		if q == "light" {
			d := "light"
			*choice = d

			// set the cookie for next time
			c.SetCookie("gowitness_theme", "light", 604800, "/", "", false, false)
			return
		}

		if q == "dark" {
			d := "dark"
			*choice = d

			// set the cookie for next time
			c.SetCookie("gowitness_theme", "dark", 604800, "/", "", false, false)
			return
		}

		// if ?theme was invalid, read the cookie value.

		cookie, err := c.Cookie("gowitness_theme")
		if err != nil {
			d := "dark"
			*choice = d

			// set the cookie for next time
			c.SetCookie("gowitness_theme", "dark", 604800, "/", "", false, false)
			return
		}

		if cookie == "light" {
			d := "light"
			*choice = d
		}

		if cookie == "dark" {
			d := "dark"
			*choice = d
		}

		// no change with an invalid value
		return
	}
}

// reporting web ui handlers
// --

// dashboardHandler handles dashboard requests
func dashboardHandler(c *gin.Context) {

	// get the sqlite db size
	var size int64
	rsDB.Raw("SELECT page_count * page_size as size FROM pragma_page_count(), pragma_page_size();").Take(&size)

	// count some statistics

	var urlCount int64
	rsDB.Model(&storage.URL{}).Count(&urlCount)

	var certCount int64
	rsDB.Model(&storage.TLS{}).Count(&certCount)

	var certDNSNameCount int64
	rsDB.Model(&storage.TLSCertificateDNSName{}).Count(&certDNSNameCount)

	var headerCount int64
	rsDB.Model(&storage.Header{}).Count(&headerCount)

	var techCount int64
	rsDB.Model(&storage.Technologie{}).Distinct().Count(&techCount)

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"DBSzie":       fmt.Sprintf("%.2f", float64(size)/1e6),
		"URLCount":     urlCount,
		"CertCount":    certCount,
		"DNSNameCount": certDNSNameCount,
		"HeaderCount":  headerCount,
		"TechCount":    techCount,
	})
}

// getSubmitHandler handles generating the view to submit urls
func getSubmitHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "submit.html", nil)
}

// submitHandler handles url submissions
func submitHandler(c *gin.Context) {

	// prepare target
	url, err := url.Parse(strings.TrimSpace(c.PostForm("url")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	if !options.AllowInsecureURIs {
		if !strings.HasPrefix(url.Scheme, "http") {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "only http(s) urls are accepted",
			})
			return
		}
	}

	fn := lib.SafeFileName(url.String())
	fp := lib.ScreenshotPath(fn, url, options.ScreenshotPath)

	preflight, err := chrm.Preflight(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	result, err := chrm.Screenshot(url)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	var rid uint
	if rsDB != nil {
		if rid, err = chrm.StoreRequest(rsDB, preflight, result, fn); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": err.Error(),
			})
			sentry.CaptureException(err)
			return
		}
	}

	if err := ioutil.WriteFile(fp, result.Screenshot, 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	if rid > 0 {
		c.Redirect(http.StatusMovedPermanently, "/details/"+strconv.Itoa(int(rid)))
		return
	}

	c.Redirect(http.StatusMovedPermanently, "/submit")
}

// detailHandler gets all of the details for a particular url id
func detailHandler(c *gin.Context) {

	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	var url storage.URL
	rsDB.
		Preload("Headers").
		Preload("TLS").
		Preload("TLS.TLSCertificates").
		Preload("TLS.TLSCertificates.DNSNames").
		Preload("Technologies").
		Preload("Console").
		Preload("Network", func(db *gorm.DB) *gorm.DB {
			db = db.Order("Time asc")
			return db
		}).
		First(&url, id)

	// get pagination limits
	var max uint
	rsDB.Model(storage.URL{}).Select("max(id)").First(&max)

	previous := url.ID
	next := url.ID

	if previous > 0 {
		previous = previous - 1
	}

	if next < max {
		next = next + 1
	}

	c.HTML(http.StatusOK, "detail.html", gin.H{
		"ID":       id,
		"Data":     url,
		"Previous": previous,
		"Next":     next,
		"Max":      max,
	})
}

// detailDOMDownloadHandler downloads the DOM as a text
func detailDOMDownloadHandler(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	var url storage.URL
	if err := rsDB.First(&url, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	c.Writer.Header().Set("Content-Disposition", `attachment; filename="`+url.Filename+`.txt"`)
	c.String(http.StatusOK, url.DOM)
}

// tableHandler handles the URL table view
func tableHandler(c *gin.Context) {

	var urls []storage.URL
	rsDB.Preload("Network").Preload("Console").Preload("Technologies").Find(&urls)

	c.HTML(http.StatusOK, "table.html", gin.H{
		"Data": urls,
	})
}

// galleryHandler handles the index page. this is the main gallery view
func galleryHandler(c *gin.Context) {

	currPage, limit, err := getPageLimit(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	pager := &lib.Pagination{
		DB:       rsDB,
		CurrPage: currPage,
		Limit:    limit,
	}

	// perception hashing
	if strings.TrimSpace(c.Query("perception_sort")) == "true" {
		pager.OrderBy = []string{"perception_hash desc"}
	}

	var urls []storage.URL
	page, err := pager.Page(&urls)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	c.HTML(http.StatusOK, "gallery.html", gin.H{
		"Data": page,
	})
}

// searchHandler handles report searching
func searchHandler(c *gin.Context) {

	query := c.PostForm("search_query")

	if query == "" {
		c.HTML(http.StatusOK, "search.html", nil)
		return
	}

	// sql friendly string search
	search := "%" + query + "%"

	// urls
	var urls []storage.URL
	rsDB.
		Where("URL LIKE ?", search).
		Or("Title LIKE ?", search).
		Or("DOM LIKE ?", search).
		Find(&urls)

	// urgh, for these relations it seems like we need to count
	// and then select? :|

	// technologies
	var technologies []storage.URL
	var technologiesCount int64
	rsDB.Model(storage.Technologie{}).Where("Value LIKE ?", search).Count(&technologiesCount)
	if technologiesCount > 0 {
		rsDB.Preload("Technologies", "Value LIKE ?", search).Find(&technologies)
	}

	// headers
	var headers []storage.URL
	var headersCount int64
	rsDB.Model(storage.Header{}).Where("Key LIKE ? OR Value LIKE ?", search, search).Count(&headersCount)
	if headersCount > 0 {
		rsDB.Preload("Headers", "Key LIKE ? OR Value LIKE ?", search, search).Find(&headers)
	}

	// console logs
	var console []storage.URL
	var consoleCount int64
	rsDB.Model(storage.ConsoleLog{}).Where("Type LIKE ? OR Value LIKE ?", search, search).Count(&consoleCount)
	if consoleCount > 0 {
		rsDB.Preload("Console", "Type LIKE ? OR Value LIKE ?", search, search).Find(&console)
	}

	// network logs
	var network []storage.URL
	var networkCount int64
	rsDB.Model(storage.NetworkLog{}).Where("URL LIKE ? OR IP LIKE ? OR Error LIKE ?", search, search, search).Count(&networkCount)
	if networkCount > 0 {
		rsDB.Preload("Network", "URL LIKE ? OR IP LIKE ? OR Error LIKE ?", search, search, search).Find(&network)
	}

	c.HTML(http.StatusOK, "search.html", gin.H{
		"Term":         query,
		"URLS":         urls,
		"Tech":         technologies,
		"TechCount":    technologiesCount,
		"Headers":      headers,
		"HeadersCount": headersCount,
		"Console":      console,
		"ConsoleCount": consoleCount,
		"Network":      network,
		"NetworkCount": networkCount,
	})
}

// getPageLimit gets the limit and page query string values from a request
func getPageLimit(c *gin.Context) (page int, limit int, err error) {

	pageS := strings.TrimSpace(c.Query("page"))
	limitS := strings.TrimSpace(c.Query("limit"))

	if pageS == "" {
		pageS = "-1"
	}
	if limitS == "" {
		limitS = "0"
	}

	page, err = strconv.Atoi(pageS)
	if err != nil {
		return
	}
	limit, err = strconv.Atoi(limitS)
	if err != nil {
		return
	}

	return
}

// API request handlers follow here
// --

// apiURLHandler returns the list of URLS in the database
func apiURLHandler(c *gin.Context) {

	// use gorm SmartSelect Fields to filter URL
	type apiURL struct {
		ID           uint64
		URL          string
		FinalURL     string
		ResponseCode int
		Title        string
	}

	var urls []apiURL
	rsDB.Model(&storage.URL{}).Find(&urls)

	c.JSON(http.StatusOK, urls)
}

func apiSearchHandler(c *gin.Context) {

	// use gorm SmartSelect Fields to filter URL
	search := "%" + c.Query("q") + "%"
	var urls []storage.URL

	rsDB.
		Where("URL LIKE ?", search).
		Or("Title LIKE ?", search).
		Or("DOM LIKE ?", search).
		Find(&urls)

	c.JSON(http.StatusOK, urls)
}

// apiDetailHandler handles a detail request for screenshot information
func apiDetailHandler(c *gin.Context) {

	var url storage.URL
	rsDB.
		Preload("Headers").
		Preload("TLS").
		Preload("TLS.TLSCertificates").
		Preload("TLS.TLSCertificates.DNSNames").
		Preload("Technologies").
		Preload("Console").
		Preload("Network").
		First(&url, c.Param("id"))

	if url.ID == 0 {
		c.JSON(http.StatusNotFound, nil)
		return
	}

	c.JSON(http.StatusOK, url)
}

func apiStatusHandler(c *gin.Context) {

	var url storage.URL
	rsDB.Select("id").Where(&storage.URL{UUIDv4: c.Param("uuid")}).First(&url)

	if url.ID == 0 {
		c.JSON(http.StatusNotFound, nil)
		return
	}

	c.JSON(http.StatusOK, url)
}

// apiDetailScreenshotHandler serves the screenshot for a specific url id
func apiDetailScreenshotHandler(c *gin.Context) {
	var url storage.URL
	rsDB.First(&url, c.Param("id"))

	if url.ID == 0 {
		c.JSON(http.StatusNotFound, nil)
		return
	}

	p := options.ScreenshotPath + "/" + url.Filename

	screenshot, err := ioutil.ReadFile(p)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"stauts": "errir",
			"error":  err.Error(),
		})
		sentry.CaptureException(err)
	}

	c.Data(http.StatusOK, "image/png", screenshot)
}

// apiScreenshot takes a screenshot of a URL
func apiScreenshotHandler(c *gin.Context) {

	type Request struct {
		URL     string   `json:"url"`
		Headers []string `json:"headers"`
		// set oneshot to "true" if you just want to see the screenshot, and not add it to the report
		OneShot string `json:"oneshot"`
		UUIDv4  string `json:uuidv4`
	}

	var requestData Request
	if err := c.ShouldBindJSON(&requestData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"status": "error",
			"error":  err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	targetURL, err := url.Parse(requestData.URL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	if !options.AllowInsecureURIs {
		if !strings.HasPrefix(targetURL.Scheme, "http") {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": "only http(s) urls are accepted",
			})
			return
		}
	}

	uuidv4, err := uuid.Parse(requestData.UUIDv4)
	if err != nil {
		uuidv4 = uuid.New()
	}

	// prepare request headers
	if len(requestData.Headers) > 0 {
		chrm.Headers = requestData.Headers
	}
	chrm.PrepareHeaderMap()

	// deliver a oneshot screenshot to the user
	if requestData.OneShot == "true" {
		result, err := chrm.Screenshot(targetURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"status":  "error",
				"message": err.Error(),
			})
			return
		}

		c.Data(http.StatusOK, "image/png", result.Screenshot)
		return
	}

	// queue a fetch session for the url
	if err = options.PrepareScreenshotPath(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status":  "error",
			"message": err.Error(),
		})
		sentry.CaptureException(err)
		return
	}

	go func(u *url.URL) {
		p := &lib.Processor{
			Logger:         options.Logger,
			Db:             rsDB,
			Chrome:         chrm,
			URL:            u,
			ScreenshotPath: options.ScreenshotPath,
			UUIDv4:         uuidv4.String(),
		}

		p.Gowitness()
	}(targetURL)

	c.JSON(http.StatusCreated, gin.H{
		"status": "created",
	})
}
