package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
)

// ── Embed all assets into the binary ─────────────────────────────────────────
//
//go:embed templates static
var embeddedFS embed.FS

// ── Models ────────────────────────────────────────────────────────────────────

type User struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Password  string    `json:"password"`
	CreatedAt time.Time `json:"created_at"`
}

type Post struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Slug       string    `json:"slug"`
	Content    string    `json:"content"`
	Excerpt    string    `json:"excerpt"`
	CoverEmoji string    `json:"cover_emoji"`
	Tags       []string  `json:"tags"`
	Published  bool      `json:"published"`
	AuthorID   string    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Views      int       `json:"views"`
	Likes      int       `json:"likes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Comment struct {
	ID         string    `json:"id"`
	PostID     string    `json:"post_id"`
	AuthorID   string    `json:"author_id"`
	AuthorName string    `json:"author_name"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type Database struct {
	Users    []*User    `json:"users"`
	Posts    []*Post    `json:"posts"`
	Comments []*Comment `json:"comments"`
}

// ── JSON file store ───────────────────────────────────────────────────────────

const dbFile = "blog_data.json"

var dbMu sync.RWMutex

func loadDB() *Database {
	dbMu.RLock()
	defer dbMu.RUnlock()
	f, err := os.Open(dbFile)
	if err != nil {
		return &Database{Users: []*User{}, Posts: []*Post{}, Comments: []*Comment{}}
	}
	defer f.Close()
	var db Database
	if err := json.NewDecoder(f).Decode(&db); err != nil {
		return &Database{Users: []*User{}, Posts: []*Post{}, Comments: []*Comment{}}
	}
	if db.Users == nil { db.Users = []*User{} }
	if db.Posts == nil { db.Posts = []*Post{} }
	if db.Comments == nil { db.Comments = []*Comment{} }
	return &db
}

func saveDB(db *Database) {
	dbMu.Lock()
	defer dbMu.Unlock()
	f, err := os.Create(dbFile)
	if err != nil { return }
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	enc.Encode(db)
}

// ── Auth / Sessions ───────────────────────────────────────────────────────────

var sessionSecret = []byte("goblog-hmac-secret-change-in-prod")

type contextKey string
const (
	ctxUserID   contextKey = "uid"
	ctxUsername contextKey = "uname"
)

func signSession(val string) string {
	h := hmac.New(sha256.New, sessionSecret)
	h.Write([]byte(val))
	return base64.URLEncoding.EncodeToString(h.Sum(nil))
}

func setSession(w http.ResponseWriter, userID, username string) {
	payload := fmt.Sprintf("%s|%s|%d", userID, username, time.Now().Unix())
	sig := signSession(payload)
	cookie := base64.URLEncoding.EncodeToString([]byte(payload + "." + sig))
	http.SetCookie(w, &http.Cookie{Name: "s", Value: cookie, Path: "/", MaxAge: 86400 * 7, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "s", Value: "", Path: "/", MaxAge: -1})
}

func getSession(r *http.Request) (userID, username string) {
	c, err := r.Cookie("s")
	if err != nil { return "", "" }
	raw, err := base64.URLEncoding.DecodeString(c.Value)
	if err != nil { return "", "" }
	parts := strings.SplitN(string(raw), ".", 2)
	if len(parts) != 2 { return "", "" }
	payload, sig := parts[0], parts[1]
	if signSession(payload) != sig { return "", "" }
	fields := strings.SplitN(payload, "|", 3)
	if len(fields) < 2 { return "", "" }
	return fields[0], fields[1]
}

func injectUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uid, uname := getSession(r)
		ctx := context.WithValue(r.Context(), ctxUserID, uid)
		ctx = context.WithValue(ctx, ctxUsername, uname)
		next(w, r.WithContext(ctx))
	}
}

func requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return injectUser(func(w http.ResponseWriter, r *http.Request) {
		if uid, _ := r.Context().Value(ctxUserID).(string); uid == "" {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	})
}

// ── Flash messages ────────────────────────────────────────────────────────────

func setFlash(w http.ResponseWriter, kind, msg string) {
	http.SetCookie(w, &http.Cookie{Name: "flash", Value: kind + ":" + msg, Path: "/", MaxAge: 5})
}
func getFlash(r *http.Request) (msg, kind string) {
	c, err := r.Cookie("flash")
	if err != nil { return "", "" }
	parts := strings.SplitN(c.Value, ":", 2)
	if len(parts) == 2 { return parts[1], parts[0] }
	return c.Value, "info"
}
func clearFlash(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "flash", Value: "", Path: "/", MaxAge: -1})
}

// ── Template engine ───────────────────────────────────────────────────────────

var funcMap = template.FuncMap{
	"truncate": func(s string, n int) string {
		r := []rune(s)
		if len(r) <= n { return s }
		return string(r[:n]) + "…"
	},
	"formatDate": func(t time.Time) string { return t.Format("Jan 02, 2006") },
	"formatFull": func(t time.Time) string { return t.Format("January 02, 2006 · 15:04") },
	"safeHTML":   func(s string) template.HTML { return template.HTML(s) },
	"add":        func(a, b int) int { return a + b },
	"lenComments": func(v []*Comment) int { return len(v) },
	"slice":      func(s string, i, j int) string {
		r := []rune(s)
		if i >= len(r) { return "" }
		if j > len(r) { j = len(r) }
		return string(r[i:j])
	},
}

type PageData struct {
	Title     string
	LoggedIn  bool
	UserID    string
	Username  string
	Flash     string
	FlashType string
	Data      interface{}
}

func render(w http.ResponseWriter, r *http.Request, tmpl, title, flash, flashType string, d interface{}) {
	uid, _ := r.Context().Value(ctxUserID).(string)
	uname, _ := r.Context().Value(ctxUsername).(string)

	pd := PageData{
		Title: title, LoggedIn: uid != "", UserID: uid, Username: uname,
		Flash: flash, FlashType: flashType, Data: d,
	}

	baseBytes, err := embeddedFS.ReadFile("templates/base.html")
	if err != nil { http.Error(w, "base template missing: "+err.Error(), 500); return }
	pageBytes, err := embeddedFS.ReadFile("templates/" + tmpl)
	if err != nil { http.Error(w, "page template missing: "+err.Error(), 500); return }

	t, err := template.New("base.html").Funcs(funcMap).Parse(string(baseBytes))
	if err != nil { http.Error(w, "base parse error: "+err.Error(), 500); return }
	t, err = t.Parse(string(pageBytes))
	if err != nil { http.Error(w, "page parse error: "+err.Error(), 500); return }

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.Execute(w, pd); err != nil {
		http.Error(w, "render error: "+err.Error(), 500)
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newID() string { b := make([]byte, 8); rand.Read(b); return hex.EncodeToString(b) }

func hashPw(pw string) string {
	h := sha256.New(); h.Write([]byte("goblog-v1-" + pw)); return hex.EncodeToString(h.Sum(nil))
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)
func slugify(s string) string {
	s = strings.ToLower(s)
	s = slugRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	b := make([]byte, 3); rand.Read(b)
	return fmt.Sprintf("%s-%s", s, hex.EncodeToString(b))
}

var emojiList = []string{"✨","🚀","💡","🌟","📖","🎯","🔥","💎","🌈","⚡","🎨","🏆","🌿","🦋","🎭","🔮","🌸","💫","🎪","🌊"}
func pickEmoji(title string) string {
	var sum int
	for _, r := range title { if unicode.IsLetter(r) { sum += int(r) } }
	return emojiList[sum%len(emojiList)]
}

func commentsFor(db *Database, postID string) []*Comment {
	var out []*Comment
	for _, c := range db.Comments { if c.PostID == postID { out = append(out, c) } }
	return out
}

// ── Handlers ──────────────────────────────────────────────────────────────────

func homeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" { http.NotFound(w, r); return }
	db := loadDB()
	var pub []*Post
	for i := len(db.Posts) - 1; i >= 0; i-- {
		if db.Posts[i].Published { pub = append(pub, db.Posts[i]) }
	}
	flash, ft := getFlash(r); clearFlash(w)
	render(w, r, "home.html", "GoBlog — Share Your Ideas With The World", flash, ft, map[string]interface{}{
		"Posts": pub, "TotalPosts": len(pub),
	})
}

func viewPostHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/post/")
	db := loadDB()
	var post *Post
	for _, p := range db.Posts { if p.ID == id && p.Published { post = p; break } }
	if post == nil { http.NotFound(w, r); return }
	post.Views++; saveDB(db)
	comments := commentsFor(db, id)
	flash, ft := getFlash(r); clearFlash(w)
	render(w, r, "post.html", post.Title+" — GoBlog", flash, ft, map[string]interface{}{
		"Post": post, "Comments": comments,
	})
}

func registerHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		flash, ft := getFlash(r); clearFlash(w)
		render(w, r, "register.html", "Create Account — GoBlog", flash, ft, nil); return
	}
	r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	if len(username) < 3 || len(email) < 5 || len(password) < 6 {
		render(w, r, "register.html", "Create Account — GoBlog", "Username ≥3 chars, password ≥6 chars required.", "error", nil); return
	}
	db := loadDB()
	for _, u := range db.Users {
		if u.Email == email || u.Username == username {
			render(w, r, "register.html", "Create Account — GoBlog", "Username or email already taken.", "error", nil); return
		}
	}
	user := &User{ID: newID(), Username: username, Email: email, Password: hashPw(password), CreatedAt: time.Now()}
	db.Users = append(db.Users, user)
	saveDB(db)
	setSession(w, user.ID, user.Username)
	setFlash(w, "success", "Welcome to GoBlog, "+username+"! 🎉")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		flash, ft := getFlash(r); clearFlash(w)
		render(w, r, "login.html", "Login — GoBlog", flash, ft, nil); return
	}
	r.ParseForm()
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	db := loadDB()
	for _, u := range db.Users {
		if u.Email == email && u.Password == hashPw(password) {
			setSession(w, u.ID, u.Username)
			setFlash(w, "success", "Welcome back, "+u.Username+"! 👋")
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther); return
		}
	}
	render(w, r, "login.html", "Login — GoBlog", "Invalid email or password.", "error", nil)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	clearSession(w); setFlash(w, "info", "You've been logged out.")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func dashboardHandler(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(ctxUserID).(string)
	db := loadDB()
	var posts []*Post
	totalViews, totalLikes := 0, 0
	for i := len(db.Posts) - 1; i >= 0; i-- {
		p := db.Posts[i]
		if p.AuthorID == uid { posts = append(posts, p); totalViews += p.Views; totalLikes += p.Likes }
	}
	flash, ft := getFlash(r); clearFlash(w)
	render(w, r, "dashboard.html", "Dashboard — GoBlog", flash, ft, map[string]interface{}{
		"Posts": posts, "TotalViews": totalViews, "TotalLikes": totalLikes, "TotalPosts": len(posts),
	})
}

func newPostHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		render(w, r, "post_form.html", "New Post — GoBlog", "", "", map[string]interface{}{"Post": nil}); return
	}
	uid, _ := r.Context().Value(ctxUserID).(string)
	uname, _ := r.Context().Value(ctxUsername).(string)
	r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	tagsRaw := r.FormValue("tags")
	published := r.FormValue("published") == "on"
	if title == "" || content == "" {
		render(w, r, "post_form.html", "New Post — GoBlog", "Title and content are required.", "error", map[string]interface{}{"Post": nil}); return
	}
	var tags []string
	for _, t := range strings.Split(tagsRaw, ",") { if t = strings.TrimSpace(t); t != "" { tags = append(tags, t) } }
	runes := []rune(content); exc := string(runes)
	if len(runes) > 180 { exc = string(runes[:180]) + "…" }
	post := &Post{
		ID: newID(), Title: title, Slug: slugify(title), Content: content, Excerpt: exc,
		CoverEmoji: pickEmoji(title), Tags: tags, Published: published,
		AuthorID: uid, AuthorName: uname, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	db := loadDB(); db.Posts = append(db.Posts, post); saveDB(db)
	if published { setFlash(w, "success", "Post published! 🚀") } else { setFlash(w, "info", "Saved as draft.") }
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func editPostHandler(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(ctxUserID).(string)
	id := strings.TrimPrefix(r.URL.Path, "/dashboard/posts/")
	id = strings.TrimSuffix(id, "/edit")
	db := loadDB()
	for _, p := range db.Posts {
		if p.ID == id && p.AuthorID == uid {
			render(w, r, "post_form.html", "Edit Post — GoBlog", "", "", map[string]interface{}{"Post": p}); return
		}
	}
	http.NotFound(w, r)
}

func updatePostHandler(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(ctxUserID).(string)
	id := strings.TrimPrefix(r.URL.Path, "/dashboard/posts/")
	id = strings.TrimSuffix(id, "/update")
	r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	content := strings.TrimSpace(r.FormValue("content"))
	tagsRaw := r.FormValue("tags")
	published := r.FormValue("published") == "on"
	var tags []string
	for _, t := range strings.Split(tagsRaw, ",") { if t = strings.TrimSpace(t); t != "" { tags = append(tags, t) } }
	runes := []rune(content); exc := string(runes)
	if len(runes) > 180 { exc = string(runes[:180]) + "…" }
	db := loadDB()
	for _, p := range db.Posts {
		if p.ID == id && p.AuthorID == uid {
			p.Title = title; p.Slug = slugify(title); p.Content = content
			p.Excerpt = exc; p.Tags = tags; p.Published = published; p.UpdatedAt = time.Now()
			saveDB(db); setFlash(w, "success", "Post updated! ✨")
			http.Redirect(w, r, "/dashboard", http.StatusSeeOther); return
		}
	}
	http.NotFound(w, r)
}

func deletePostHandler(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(ctxUserID).(string)
	id := strings.TrimPrefix(r.URL.Path, "/dashboard/posts/")
	id = strings.TrimSuffix(id, "/delete")
	db := loadDB()
	var newPosts []*Post
	for _, p := range db.Posts { if !(p.ID == id && p.AuthorID == uid) { newPosts = append(newPosts, p) } }
	var newComments []*Comment
	for _, c := range db.Comments { if c.PostID != id { newComments = append(newComments, c) } }
	db.Posts = newPosts; db.Comments = newComments; saveDB(db)
	setFlash(w, "info", "Post deleted.")
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func commentHandler(w http.ResponseWriter, r *http.Request) {
	uid, _ := r.Context().Value(ctxUserID).(string)
	uname, _ := r.Context().Value(ctxUsername).(string)
	postID := strings.TrimPrefix(r.URL.Path, "/post/")
	postID = strings.TrimSuffix(postID, "/comment")
	r.ParseForm()
	content := strings.TrimSpace(r.FormValue("content"))
	if content == "" { http.Redirect(w, r, "/post/"+postID, http.StatusSeeOther); return }
	db := loadDB()
	db.Comments = append(db.Comments, &Comment{
		ID: newID(), PostID: postID, AuthorID: uid, AuthorName: uname, Content: content, CreatedAt: time.Now(),
	})
	saveDB(db); setFlash(w, "success", "Comment added!")
	http.Redirect(w, r, "/post/"+postID, http.StatusSeeOther)
}

func likeHandler(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/post/")
	id = strings.TrimSuffix(id, "/like")
	db := loadDB()
	for _, p := range db.Posts { if p.ID == id { p.Likes++; break } }
	saveDB(db); http.Redirect(w, r, "/post/"+id, http.StatusSeeOther)
}

func profileHandler(w http.ResponseWriter, r *http.Request) {
	uname := strings.TrimPrefix(r.URL.Path, "/author/")
	db := loadDB()
	var author *User
	for _, u := range db.Users { if u.Username == uname { author = u; break } }
	if author == nil { http.NotFound(w, r); return }
	var posts []*Post
	for i := len(db.Posts) - 1; i >= 0; i-- {
		if db.Posts[i].AuthorID == author.ID && db.Posts[i].Published { posts = append(posts, db.Posts[i]) }
	}
	render(w, r, "profile.html", author.Username+" — GoBlog", "", "", map[string]interface{}{
		"Author": author, "Posts": posts,
	})
}

// ── Main ──────────────────────────────────────────────────────────────────────

func main() {
	mux := http.NewServeMux()

	// Static files served from embedded FS
	staticFS, err := fs.Sub(embeddedFS, "static")
	if err != nil { log.Fatal(err) }
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Routes
	mux.HandleFunc("/", injectUser(homeHandler))
	mux.HandleFunc("/register", injectUser(registerHandler))
	mux.HandleFunc("/login", injectUser(loginHandler))
	mux.HandleFunc("/logout", injectUser(logoutHandler))
	mux.HandleFunc("/author/", injectUser(profileHandler))

	// Post routes
	mux.HandleFunc("/post/", injectUser(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/comment") && r.Method == http.MethodPost:
			requireAuth(commentHandler)(w, r)
		case strings.HasSuffix(path, "/like") && r.Method == http.MethodPost:
			likeHandler(w, r)
		default:
			viewPostHandler(w, r)
		}
	}))

	// Dashboard routes (all protected)
	mux.HandleFunc("/dashboard", requireAuth(dashboardHandler))
	mux.HandleFunc("/dashboard/posts/new", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost { newPostHandler(w, r) } else { newPostHandler(w, r) }
	}))
	mux.HandleFunc("/dashboard/posts/", requireAuth(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case strings.HasSuffix(path, "/edit"):
			editPostHandler(w, r)
		case strings.HasSuffix(path, "/update") && r.Method == http.MethodPost:
			updatePostHandler(w, r)
		case strings.HasSuffix(path, "/delete") && r.Method == http.MethodPost:
			deletePostHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}))

	log.Println("╔══════════════════════════════════════╗")
	log.Println("║  🚀 GoBlog running!                  ║")
	log.Println("║  Open: http://localhost:8080          ║")
	log.Println("╚══════════════════════════════════════╝")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
