## v0.7.2 (2026-09-20)

* refactor: share the renderers and the render cache (32bb8af)

## v0.7.1 (2026-09-20)

* refactor(state): keep the settings in the shared dir (765d52e)
* refactor(search): parse terms with the shared matcher (691a510)

## v0.7.0 (2026-09-19)

* feat(ui): move the counter under the list (0522c99)

## v0.6.2 (2026-09-19)

* refactor: share the home path helpers (0f9bafc)

## v0.6.1 (2026-09-19)

* refactor: share the last duplicated helpers (a4e2940)

## v0.6.0 (2026-09-19)

* feat(ui): placeholder in the filter, name only standalone (f7e08cb)
* feat(ui): page the list, jump to its ends, expand the help (3c4b4a9)
* fix(filter): stop scoring short texts higher (6209287)
* refactor(ui): one highlight implementation for the family (a1a6946)
* fix(filter): match the query where it occurs whole (234235b)

## v0.5.0 (2026-09-19)

* feat(ui): show the list's position under the list (8fea744)
* docs(dev): link the plugin from the checkout with $PWD (63faec4)
* test(pty): wait for the preview before reading the scoped log frame (8fae4cc)
* ci: spend less time on CI and on releases (93049cf)

## v0.4.0 (2026-09-19)

* test(pty): run the TUI check without delta (74025a8)
* feat: support linux and share the release process (1350767)
* docs: refer to the sibling tools by their new names (d3b3b60)

## v0.3.0 (2026-09-19)

* feat(preview): ignore whitespace changes with ctrl+s (063a906)
* test(hunk): share the hunk renderer's tests with gotochanged (1ebcf40)
* docs(demo): add the demo GIF and its scenario (cded1d7)

## v0.2.0 (2026-09-18)

* feat(ui): quit with q on an empty filter (95fc905)

## v0.1.0 (2026-09-18)

* ci: run gofmt, vet and tests on push (5cce4a8)
* chore: add the MIT license (ebb074e)
* feat(ui): single frame, hunk renderer, render cache (003deca)
* fix(ui): list commits top-down under the filter input (535c384)
* feat(ui): rework the layout and extend log browsing (d783f49)
* feat: add git log browser tui and herdr plugin (66396c0)

