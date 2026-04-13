<p align="center">
  <img src="https://raw.githubusercontent.com/chamodyashakunthala/GoBlog/main/GoBlog.jpeg" width ="auto" alt="Profile Banner" />
</p>


# GoBlog — Modern Blog Platform

Zero external dependencies. Just pure Go standard library.

## Run (requires Go 1.21+)

```bash
go run main.go
```

Then open: http://localhost:8080

## Features
- Register, login, logout
- Create, edit, delete posts (with draft/publish toggle)
- Tags, emoji covers, view counter, likes
- Comments
- Author profile pages
- Responsive, beautiful UI
- Data stored in `blog_data.json` (auto-created)

## Project Structure
```
goblog/
├── main.go          ← Everything: server, handlers, models, auth
├── go.mod           ← Zero dependencies
├── templates/       ← HTML templates (embedded into binary)
│   ├── base.html
│   ├── home.html
│   ├── post.html
│   ├── post_form.html
│   ├── dashboard.html
│   ├── login.html
│   ├── register.html
│   └── profile.html
└── static/          ← CSS & JS (embedded into binary)
    ├── css/style.css
    └── js/app.js
```
