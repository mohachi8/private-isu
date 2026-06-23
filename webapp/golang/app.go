package main

import (
	"context"
	crand "crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	mysql "github.com/go-sql-driver/mysql"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/jmoiron/sqlx"
)

var (
	db    *sqlx.DB
	store sessions.Store
)

const (
	postsPerPage = 20
	// Fetch a buffer of newest posts so makePosts can drop posts by del_flg=1
	// users and still have >= postsPerPage to show. Joining users to filter in
	// SQL forces a full users scan + filesort (much slower than reading a few
	// extra rows via the created_at index), so we filter in Go instead.
	fetchPostsLimit = postsPerPage * 3
	ISO8601Format   = "2006-01-02T15:04:05-07:00"
	UploadLimit     = 10 * 1024 * 1024 // 10mb
)

type User struct {
	ID          int       `db:"id"`
	AccountName string    `db:"account_name"`
	Passhash    string    `db:"passhash"`
	Authority   int       `db:"authority"`
	DelFlg      int       `db:"del_flg"`
	CreatedAt   time.Time `db:"created_at"`
}

type Post struct {
	ID           int       `db:"id"`
	UserID       int       `db:"user_id"`
	Imgdata      []byte    `db:"imgdata"`
	Body         string    `db:"body"`
	Mime         string    `db:"mime"`
	CreatedAt    time.Time `db:"created_at"`
	CommentCount int
	Comments     []Comment
	User         User
	CSRFToken    string
	ImageURL     string
	CreatedAtISO string
}

type Comment struct {
	ID        int       `db:"id"`
	PostID    int       `db:"post_id"`
	UserID    int       `db:"user_id"`
	Comment   string    `db:"comment"`
	CreatedAt time.Time `db:"created_at"`
	User      User
}

func init() {
	// Signed cookie sessions: no per-request memcached round-trip. Session data
	// (user_id, csrf_token, flash) is tamper-proof via HMAC; nothing secret.
	cs := sessions.NewCookieStore([]byte("sendagaya"))
	cs.Options = &sessions.Options{Path: "/", MaxAge: 86400 * 30, HttpOnly: true}
	// Use a JSON serializer instead of gob (gob recompiles its type descriptor
	// on every request; the session is read on every request).
	for _, c := range cs.Codecs {
		if sc, ok := c.(*securecookie.SecureCookie); ok {
			sc.SetSerializer(jsonSessionSerializer{})
		}
	}
	store = cs
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
}

func dbInitialize(ctx context.Context) {
	sqls := []string{
		"DELETE FROM users WHERE id > 1000",
		"DELETE FROM posts WHERE id > 10000",
		"DELETE FROM comments WHERE id > 100000",
		"UPDATE users SET del_flg = 0",
		"UPDATE users SET del_flg = 1 WHERE id % 50 = 0",
	}

	for _, sql := range sqls {
		db.ExecContext(ctx, sql)
	}
}

func tryLogin(ctx context.Context, accountName, password string) *User {
	u, ok := userByName(accountName)
	if !ok || u.DelFlg != 0 {
		return nil
	}

	if calculatePasshash(ctx, u.AccountName, password) == u.Passhash {
		return &u
	} else {
		return nil
	}
}

var (
	accountNameRe = regexp.MustCompile(`\A[0-9a-zA-Z_]{3,}\z`)
	passwordRe    = regexp.MustCompile(`\A[0-9a-zA-Z_]{6,}\z`)
)

func validateUser(accountName, password string) bool {
	return accountNameRe.MatchString(accountName) && passwordRe.MatchString(password)
}

// digest returns the lowercase hex SHA-512 of src. This replaces the original
// implementation that shelled out to `openssl dgst -sha512` per call (a process
// spawn on every login/register); the output is byte-for-byte identical so
// existing password hashes remain valid.
func digest(ctx context.Context, src string) string {
	sum := sha512.Sum512([]byte(src))
	return hex.EncodeToString(sum[:])
}

func calculateSalt(ctx context.Context, accountName string) string {
	return digest(ctx, accountName)
}

func calculatePasshash(ctx context.Context, accountName, password string) string {
	return digest(ctx, password+":"+calculateSalt(ctx, accountName))
}

func getSession(r *http.Request) *sessions.Session {
	session, _ := store.Get(r, "isuconp-go.session")

	return session
}

func getSessionUser(r *http.Request) User {
	session := getSession(r)
	uid, ok := session.Values["user_id"]
	if !ok || uid == nil {
		return User{}
	}

	var id int
	switch v := uid.(type) {
	case int:
		id = v
	case int64:
		id = int(v)
	case float64: // JSON-decoded session value
		id = int(v)
	default:
		return User{}
	}

	u, ok := userByID(id)
	if !ok {
		return User{}
	}
	return u
}

func getFlash(w http.ResponseWriter, r *http.Request, key string) string {
	session := getSession(r)
	value, ok := session.Values[key]

	if !ok || value == nil {
		return ""
	} else {
		delete(session.Values, key)
		session.Save(r, w)
		return value.(string)
	}
}

// makePosts assembles posts with their comments and authors entirely from the
// in-memory comment and user caches — no DB queries — eliminating what used to
// be the dominant query cost (SELECT comments + SELECT users).
func makePosts(ctx context.Context, results []Post, csrfToken string, allComments bool) ([]Post, error) {
	posts := make([]Post, 0, len(results))

	for _, p := range results {
		author, ok := userByID(p.UserID)
		if !ok || author.DelFlg != 0 {
			continue
		}

		limit := 3
		if allComments {
			limit = 0 // all
		}
		count, cs := commentSummary(p.ID, limit)
		p.CommentCount = count
		for i := range cs {
			cs[i].User, _ = userByID(cs[i].UserID)
		}
		p.Comments = cs

		p.User = author
		p.CSRFToken = csrfToken
		// Precompute the image URL and formatted timestamp so the template reads
		// fields instead of invoking a func / time.Format method via reflection
		// per post (the dominant render cost).
		p.ImageURL = imageURL(p)
		p.CreatedAtISO = p.CreatedAt.Format(ISO8601Format)

		posts = append(posts, p)
		if len(posts) >= postsPerPage {
			break
		}
	}

	return posts, nil
}

// imageDir is where image files are materialized so nginx can serve them
// statically (see infra/nginx: location /image/ { try_files $uri @app; }).
// Path is relative to the Go app's working dir (webapp/golang).
const imageDir = "../public/image"

func extFromMime(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	}
	return ""
}

// writeImageFile materializes an image to disk so subsequent requests are
// served by nginx without hitting the app/DB. Best-effort: errors are logged
// but not fatal (the app can still serve the bytes directly).
func writeImageFile(pid int, mime string, data []byte) error {
	ext := extFromMime(mime)
	if ext == "" {
		return fmt.Errorf("unknown mime: %s", mime)
	}
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		log.Print(err)
		return err
	}
	// Write atomically (temp + rename) so a concurrent GET never sees a
	// partially-written file.
	path := fmt.Sprintf("%s/%d%s", imageDir, pid, ext)
	tmp := fmt.Sprintf("%s.tmp%d", path, pid)
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		log.Print(err)
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Print(err)
		os.Remove(tmp)
		return err
	}
	return nil
}

func imageURL(p Post) string {
	return "/image/" + strconv.Itoa(p.ID) + extFromMime(p.Mime)
}

func isLogin(u User) bool {
	return u.ID != 0
}

func getCSRFToken(r *http.Request) string {
	session := getSession(r)
	csrfToken, ok := session.Values["csrf_token"]
	if !ok {
		return ""
	}
	return csrfToken.(string)
}

func secureRandomStr(b int) string {
	k := make([]byte, b)
	if _, err := crand.Read(k); err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", k)
}

func getTemplPath(filename string) string {
	return path.Join("templates", filename)
}

func getInitialize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dbInitialize(ctx)
	// Rebuild the caches: initialize resets del_flg, removes users id>1000 and
	// comments id>100000.
	if err := loadAllUsers(ctx); err != nil {
		log.Print(err)
	}
	if err := loadAllComments(ctx); err != nil {
		log.Print(err)
	}
	// Cached post fragments reference comment state, which just reset.
	clearPostFragments()
	clearDetailFragments()
	if err := loadAllPostsCache(ctx); err != nil {
		log.Print(err)
	}
	if err := loadStats(ctx); err != nil {
		log.Print(err)
	}
	// Remove run-specific uploaded image files (post id > 10000); seeded images
	// (id <= 10000) are immutable and kept so they stay materialized on disk.
	cleanupUploadedImages()
	w.WriteHeader(http.StatusOK)
}

// cleanupUploadedImages deletes materialized image files whose post id > 10000,
// matching dbInitialize which removes posts with id > 10000.
func cleanupUploadedImages() {
	entries, err := os.ReadDir(imageDir)
	if err != nil {
		return // dir may not exist yet
	}
	for _, e := range entries {
		name := e.Name()
		dot := strings.IndexByte(name, '.')
		if dot <= 0 {
			continue
		}
		id, err := strconv.Atoi(name[:dot])
		if err != nil {
			continue
		}
		if id > 10000 {
			os.Remove(fmt.Sprintf("%s/%s", imageDir, name))
		}
	}
}

func getLogin(w http.ResponseWriter, r *http.Request) {
	me := getSessionUser(r)

	if isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	loginTmpl.Execute(w, struct {
		Me    User
		Flash string
	}{me, getFlash(w, r, "notice")})
}

func postLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	u := tryLogin(ctx, r.FormValue("account_name"), r.FormValue("password"))

	if u != nil {
		session := getSession(r)
		session.Values["user_id"] = u.ID
		session.Values["csrf_token"] = secureRandomStr(16)
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
	} else {
		session := getSession(r)
		session.Values["notice"] = "アカウント名かパスワードが間違っています"
		session.Save(r, w)

		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func getRegister(w http.ResponseWriter, r *http.Request) {
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	registerTmpl.Execute(w, struct {
		Me    User
		Flash string
	}{User{}, getFlash(w, r, "notice")})
}

func postRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if isLogin(getSessionUser(r)) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	accountName, password := r.FormValue("account_name"), r.FormValue("password")

	validated := validateUser(accountName, password)
	if !validated {
		session := getSession(r)
		session.Values["notice"] = "アカウント名は3文字以上、パスワードは6文字以上である必要があります"
		session.Save(r, w)

		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	if _, taken := userByName(accountName); taken {
		session := getSession(r)
		session.Values["notice"] = "アカウント名がすでに使われています"
		session.Save(r, w)

		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}

	passhash := calculatePasshash(ctx, accountName, password)
	query := "INSERT INTO `users` (`account_name`, `passhash`) VALUES (?,?)"
	result, err := db.ExecContext(ctx, query, accountName, passhash)
	if err != nil {
		log.Print(err)
		return
	}

	uid, err := result.LastInsertId()
	if err != nil {
		log.Print(err)
		return
	}
	// Add the new user to the cache so login/post lookups see it immediately.
	cacheUser(User{ID: int(uid), AccountName: accountName, Passhash: passhash})

	session := getSession(r)
	session.Values["user_id"] = int(uid)
	session.Values["csrf_token"] = secureRandomStr(16)
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func getLogout(w http.ResponseWriter, r *http.Request) {
	session := getSession(r)
	delete(session.Values, "user_id")
	session.Options = &sessions.Options{MaxAge: -1}
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusFound)
}

func getIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)

	results := newestPosts(fetchPostsLimit)

	posts, err := makePosts(ctx, results, getCSRFToken(r), false)
	if err != nil {
		log.Print(err)
		return
	}

	csrf := getCSRFToken(r)
	indexTmpl.Execute(w, struct {
		PostsHTML template.HTML
		Me        User
		CSRFToken string
		Flash     string
	}{renderPostList(posts, csrf), me, csrf, getFlash(w, r, "notice")})
}

func getAccountName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accountName := r.PathValue("accountName")

	user, ok := userByName(accountName)
	if !ok || user.DelFlg != 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	results := userPosts(user.ID, postsPerPage)

	posts, err := makePosts(ctx, results, getCSRFToken(r), false)
	if err != nil {
		log.Print(err)
		return
	}

	// All three aggregates come from in-memory caches (no DB).
	postCount, commentCount, commentedCount := userStats(user.ID)

	me := getSessionUser(r)

	accountTmpl.Execute(w, struct {
		PostsHTML      template.HTML
		User           User
		PostCount      int
		CommentCount   int
		CommentedCount int
		Me             User
	}{renderPostList(posts, getCSRFToken(r)), user, postCount, commentCount, commentedCount, me})
}

func getPosts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	m, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Print(err)
		return
	}
	maxCreatedAt := m.Get("max_created_at")
	if maxCreatedAt == "" {
		return
	}

	t, err := time.Parse(ISO8601Format, maxCreatedAt)
	if err != nil {
		log.Print(err)
		return
	}

	results := postsBefore(t, fetchPostsLimit)

	posts, err := makePosts(ctx, results, getCSRFToken(r), false)
	if err != nil {
		log.Print(err)
		return
	}

	if len(posts) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Write([]byte(renderPostList(posts, getCSRFToken(r))))
}

func getPostsID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pidStr := r.PathValue("id")
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	p0, ok := postByIDCache(pid)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	results := []Post{p0}

	posts, err := makePosts(ctx, results, getCSRFToken(r), true)
	if err != nil {
		log.Print(err)
		return
	}

	if len(posts) == 0 {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	p := posts[0]

	me := getSessionUser(r)

	postIDTmpl.Execute(w, struct {
		PostHTML template.HTML
		Me       User
	}{renderPostDetail(p, getCSRFToken(r)), me})
}

func postIndex(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		session := getSession(r)
		session.Values["notice"] = "画像が必須です"
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	mime := ""
	if file != nil {
		// 投稿のContent-Typeからファイルのタイプを決定する
		contentType := header.Header["Content-Type"][0]
		if strings.Contains(contentType, "jpeg") {
			mime = "image/jpeg"
		} else if strings.Contains(contentType, "png") {
			mime = "image/png"
		} else if strings.Contains(contentType, "gif") {
			mime = "image/gif"
		} else {
			session := getSession(r)
			session.Values["notice"] = "投稿できる画像形式はjpgとpngとgifだけです"
			session.Save(r, w)

			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}

	filedata, err := io.ReadAll(file)
	if err != nil {
		log.Print(err)
		return
	}

	if len(filedata) > UploadLimit {
		session := getSession(r)
		session.Values["notice"] = "ファイルサイズが大きすぎます"
		session.Save(r, w)

		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Image bytes go to disk, not the DB. Reads come from the in-memory caches,
	// so a post is only "visible" once addPostToCache runs — do that LAST, after
	// the image file exists, so no reader can fetch the image before it's written.
	body := r.FormValue("body")
	result, err := db.ExecContext(
		ctx,
		"INSERT INTO `posts` (`user_id`, `mime`, `body`) VALUES (?,?,?)",
		me.ID,
		mime,
		body,
	)
	if err != nil {
		log.Print(err)
		return
	}
	pid, err := result.LastInsertId()
	if err != nil {
		log.Print(err)
		return
	}

	// Materialize the uploaded image first. If it fails (e.g. disk full), do NOT
	// make the post visible — a cached post whose image 404s would fail other
	// requests that render the index. Surface a 500 for this upload instead.
	if err := writeImageFile(int(pid), mime, filedata); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	statAddPost(me.ID, int(pid))
	addPostToCache(Post{ID: int(pid), UserID: me.ID, Body: body, Mime: mime, CreatedAt: time.Now()})

	http.Redirect(w, r, "/posts/"+strconv.FormatInt(pid, 10), http.StatusFound)
}

// getImage serves images from disk only. Images are materialized to
// ../public/image (seeded via bin/materialize-images.sh, uploads via postIndex)
// and nginx serves existing files directly via try_files; this handler is only
// reached on a miss. Image bytes are no longer stored in the DB.
func getImage(w http.ResponseWriter, r *http.Request) {
	pid, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	ext := r.PathValue("ext")
	var mime string
	switch ext {
	case "jpg":
		mime = "image/jpeg"
	case "png":
		mime = "image/png"
	case "gif":
		mime = "image/gif"
	default:
		w.WriteHeader(http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(fmt.Sprintf("%s/%d.%s", imageDir, pid, ext))
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", mime)
	w.Write(data)
}

func postComment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	postID, err := strconv.Atoi(r.FormValue("post_id"))
	if err != nil {
		log.Print("post_idは整数のみです")
		return
	}

	comment := r.FormValue("comment")
	query := "INSERT INTO `comments` (`post_id`, `user_id`, `comment`) VALUES (?,?,?)"
	result, err := db.ExecContext(ctx, query, postID, me.ID, comment)
	if err != nil {
		log.Print(err)
		return
	}

	// Reflect the new comment in the cache (approximate created_at with now;
	// ordering within a run stays correct).
	cid, _ := result.LastInsertId()
	addComment(Comment{ID: int(cid), PostID: postID, UserID: me.ID, Comment: comment, CreatedAt: time.Now()})
	statAddComment(me.ID)
	// The post's cached fragments (list: count+latest3, detail: all comments) are stale.
	invalidatePostFragment(postID)
	invalidateDetailFragment(postID)

	http.Redirect(w, r, fmt.Sprintf("/posts/%d", postID), http.StatusFound)
}

func getAdminBanned(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if me.Authority == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	users := []User{}
	err := db.SelectContext(ctx, &users, "SELECT * FROM `users` WHERE `authority` = 0 AND `del_flg` = 0 ORDER BY `created_at` DESC")
	if err != nil {
		log.Print(err)
		return
	}

	bannedTmpl.Execute(w, struct {
		Users     []User
		Me        User
		CSRFToken string
	}{users, me, getCSRFToken(r)})
}

func postAdminBanned(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	me := getSessionUser(r)
	if !isLogin(me) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if me.Authority == 0 {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if r.FormValue("csrf_token") != getCSRFToken(r) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		return
	}

	query := "UPDATE `users` SET `del_flg` = ? WHERE `id` = ?"

	err := r.ParseForm()
	if err != nil {
		log.Print(err)
		return
	}

	for _, id := range r.Form["uid[]"] {
		db.ExecContext(ctx, query, 1, id)
	}
	// Reflect the del_flg changes in the user cache.
	if err := loadAllUsers(ctx); err != nil {
		log.Print(err)
	}

	http.Redirect(w, r, "/admin/banned", http.StatusFound)
}

func main() {
	host := os.Getenv("ISUCONP_DB_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("ISUCONP_DB_PORT")
	if port == "" {
		port = "3306"
	}
	_, err := strconv.Atoi(port)
	if err != nil {
		log.Fatalf("Failed to read DB port number from an environment variable ISUCONP_DB_PORT.\nError: %s", err.Error())
	}
	user := os.Getenv("ISUCONP_DB_USER")
	if user == "" {
		user = "root"
	}
	password := os.Getenv("ISUCONP_DB_PASSWORD")
	dbname := os.Getenv("ISUCONP_DB_NAME")
	if dbname == "" {
		dbname = "isuconp"
	}

	cfg := mysql.NewConfig()
	cfg.User = user
	cfg.Passwd = password
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%s", host, port)
	cfg.DBName = dbname
	cfg.Params = map[string]string{
		"charset": "utf8mb4",
		// Interpolate params client-side to avoid a server round-trip per query
		// (removes the ADMIN PREPARE overhead seen in pt-query-digest).
		"interpolateParams": "true",
	}
	cfg.ParseTime = true
	cfg.Loc = time.Local
	dsn := cfg.FormatDSN()

	// pprof: always-on localhost-only profiling endpoint (:6060).
	startPprof()

	// OpenTelemetry tracing -> Jaeger. Enabled only when ENABLE_TRACING=1.
	if tracingEnabled() {
		shutdown, err := initTracer(context.Background())
		if err != nil {
			log.Printf("failed to init tracer: %s", err.Error())
		} else {
			defer shutdown(context.Background())
			log.Print("OpenTelemetry tracing enabled -> OTLP")
		}
	}

	db, err = openDB(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %s.", err.Error())
	}
	defer db.Close()

	// Reuse DB connections instead of opening one per query. Go's default
	// MaxIdleConns is 2, which forces constant connect/close churn (and TIME_WAIT
	// buildup) under load. Keep a warm pool well under MySQL max_connections(151).
	db.SetMaxOpenConns(100)
	db.SetMaxIdleConns(100)
	db.SetConnMaxLifetime(0)

	// Warm the in-memory caches before serving.
	if err := loadAllUsers(context.Background()); err != nil {
		log.Fatalf("Failed to load users: %s.", err.Error())
	}
	if err := loadAllComments(context.Background()); err != nil {
		log.Fatalf("Failed to load comments: %s.", err.Error())
	}
	if err := loadAllPostsCache(context.Background()); err != nil {
		log.Fatalf("Failed to load posts: %s.", err.Error())
	}
	if err := loadStats(context.Background()); err != nil {
		log.Fatalf("Failed to load stats: %s.", err.Error())
	}

	r := chi.NewRouter()
	// Recover from any handler panic and return 500 instead of crashing the
	// whole process (a crash would fail every in-flight + queued request until
	// systemd restarts it — thousands of cascading failures).
	r.Use(middleware.Recoverer)
	// pprof endpoint labels add a small per-request cost; enable only when
	// profiling (PPROF_LABELS=1), off by default for scoring runs.
	if os.Getenv("PPROF_LABELS") == "1" {
		r.Use(pprofLabelMiddleware)
	}

	r.Get("/initialize", getInitialize)
	r.Get("/login", getLogin)
	r.Post("/login", postLogin)
	r.Get("/register", getRegister)
	r.Post("/register", postRegister)
	r.Get("/logout", getLogout)
	r.Get("/", getIndex)
	r.Get("/posts", getPosts)
	r.Get("/posts/{id}", getPostsID)
	r.Post("/", postIndex)
	r.Get("/image/{id}.{ext}", getImage)
	r.Post("/comment", postComment)
	r.Get("/admin/banned", getAdminBanned)
	r.Post("/admin/banned", postAdminBanned)
	r.Get(`/@{accountName:[0-9a-zA-Z_]+}`, getAccountName)
	r.Mount("/", http.FileServer(http.Dir("../public")))

	log.Fatal(http.ListenAndServe(":8080", wrapHandler(r)))
}
