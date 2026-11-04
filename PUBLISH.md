# Publishing tablu.me

GitHub Pages serves the `gh-pages` branch (Settings → Pages → Deploy from a
branch → `gh-pages`, `/ (root)`). That branch is this folder at its root,
made from the branch that holds `site/`:

```bash
git subtree split --prefix=site -b gh-pages-new && git push -f origin gh-pages-new:gh-pages && git branch -D gh-pages-new
```

`CNAME` keeps the custom domain, `.nojekyll` makes Pages serve the files as
they are.
