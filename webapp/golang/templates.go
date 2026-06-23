package main

import "html/template"

// Templates are parsed once at startup instead of on every request.
// html/template values are safe for concurrent Execute after parsing.

var tmplFuncs = template.FuncMap{"imageURL": imageURL}

var (
	indexTmpl = template.Must(template.New("layout.html").Funcs(tmplFuncs).ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("index.html"),
		getTemplPath("posts.html"),
		getTemplPath("post.html"),
	))
	accountTmpl = template.Must(template.New("layout.html").Funcs(tmplFuncs).ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("user.html"),
		getTemplPath("posts.html"),
		getTemplPath("post.html"),
	))
	postsTmpl = template.Must(template.New("posts.html").Funcs(tmplFuncs).ParseFiles(
		getTemplPath("posts.html"),
		getTemplPath("post.html"),
	))
	postIDTmpl = template.Must(template.New("layout.html").Funcs(tmplFuncs).ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("post_id.html"),
		getTemplPath("post.html"),
	))
	loginTmpl = template.Must(template.ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("login.html"),
	))
	registerTmpl = template.Must(template.ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("register.html"),
	))
	bannedTmpl = template.Must(template.ParseFiles(
		getTemplPath("layout.html"),
		getTemplPath("banned.html"),
	))
)
