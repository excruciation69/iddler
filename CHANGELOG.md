# Changelog

## 5.0.0

### Added
- Inventory section: Steam, CS2, TF2, Dota 2 and Rust inventories with search, filters and sorting.
- Market prices for every item: lowest, median, highest buy order and daily volume.
- Selling: single items, a selection, or all duplicates at once. The fee is shown before listing.
- My listings: remove selected listings or all of them.
- Market search with a link to each item's page.
- Listing confirmations inside the app, manual or automatic, using the `identity_secret` from a `.maFile`.
- Sign-in with a QR code from the Steam mobile app.
- Card farming: skip list and "only these" list, editable from the queue.
- Card farming can switch to idling hours after the last drop.
- Achievements: pause range between unlocks, allowed hours, option to keep the game running.

### Changed
- The line at the top of the window no longer stutters or gets cut off when the window is moved.
- Tooltips are drawn above everything and are no longer clipped by game cards.
- Animations stop while the window is hidden or minimised.
- Large grids render only the visible part, so scrolling stays smooth with big libraries.

## 4.1.0

### Added
- Sidebar with section names and an account switcher.
- Accent colour setting.
- Favourite games.
- Support page.

### Changed
- Language switches instantly, without reloading.
- New clock, logo and app icons.
