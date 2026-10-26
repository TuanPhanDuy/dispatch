# Hướng dẫn sử dụng dispatch

[English](USER_GUIDE.md) · **Tiếng Việt**

dispatch giúp bạn chạy nhiều AI coding agent cùng lúc (Claude Code, Codex, Aider hoặc bất kỳ lệnh shell nào) và theo dõi tất cả ở một chỗ — bằng giao diện terminal (TUI) hoặc ứng dụng desktop.

Tài liệu này hướng dẫn cài đặt, cách dùng hằng ngày cho cả hai giao diện, cấu hình, cập nhật và xử lý sự cố. Bản ngắn gọn nằm trong [README](../README.md).

- [Khái niệm cốt lõi](#core-ideas)
- [Cài đặt](#install)
- [Ứng dụng desktop](#desktop-app)
- [Giao diện terminal (TUI)](#tui)
- [Nhiều account Claude](#multiple-claude-accounts)
- [Cấu hình](#configuration)
- [Cập nhật](#updating)
- [Xử lý sự cố](#troubleshooting)
- [Gỡ cài đặt](#uninstalling)

<a id="core-ideas"></a>
## Khái niệm cốt lõi

**Session chạy trong một daemon nền, không nằm trong cửa sổ của bạn.** Lần đầu mở dispatch, nó khởi động một tiến trình daemon nhỏ, nơi giữ terminal của mọi agent. TUI và ứng dụng desktop chỉ là các cửa sổ nhìn vào daemon đó. Bạn có thể đóng một trong hai, hoặc cả hai, và agent vẫn tiếp tục chạy. Mở lại dispatch thì mọi thứ vẫn còn nguyên, kèm toàn bộ lịch sử output (scrollback).

**Hai giao diện dùng chung session.** Session tạo trong ứng dụng desktop sẽ hiện trong TUI và ngược lại, vì cả hai cùng nói chuyện với một daemon.

**Mỗi session có một trạng thái:**

| Trạng thái | Ý nghĩa |
|---|---|
| Running | Agent đang in ra output. |
| Needs attention | Agent đã im lặng một lúc (mặc định 10 giây, Claude Code là 15 giây). Thường là nó đang chờ bạn trả lời. |
| Exited | Tiến trình của agent đã kết thúc. Scrollback được giữ lại cho tới khi bạn xoá session. |

"Needs attention" chỉ dựa trên thời gian: dispatch không đọc nội dung output của agent, nó chỉ nhận ra agent đã im lặng. Một agent đang suy nghĩ lâu mà không in gì cũng sẽ bị đánh dấu.

> **Quan trọng:** đóng giao diện thì luôn an toàn. Dừng *daemon* sẽ kết thúc mọi session mà nó đang giữ. Xem [Cập nhật](#updating).

<a id="install"></a>
## Cài đặt

| Bạn muốn | Tải file |
|---|---|
| Ứng dụng desktop trên macOS | `dispatch-<version>-mac-arm64.dmg` (Apple Silicon) hoặc `dispatch-<version>-mac-x64.dmg` (Intel) |
| Giao diện terminal trên macOS hoặc Linux | `dispatch_<version>_<os>_<arch>.tar.gz` |

Tất cả đều có trên [trang releases](https://github.com/TuanPhanDuy/dispatch/releases/latest). Chưa hỗ trợ Windows.

### Ứng dụng desktop

1. Mở file `.dmg` và kéo **dispatch** vào thư mục **Applications**.
2. Ứng dụng chưa được Apple notarize nên macOS sẽ chặn lần mở đầu tiên. Chuột phải vào ứng dụng → **Open** → **Open**, hoặc chạy:

   ```sh
   xattr -dr com.apple.quarantine /Applications/dispatch.app
   ```

Ứng dụng đã chứa sẵn binary `dispatch` và tự khởi động daemon. Bạn không cần cài thêm gì.

### Giao diện terminal

```sh
tar -xzf dispatch_*_$(uname -s | tr A-Z a-z)_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz
sudo mv dispatch /usr/local/bin/
dispatch --version
```

Trên macOS, nếu binary bị chặn: `xattr -d com.apple.quarantine /usr/local/bin/dispatch`.

Nếu đã cài Go, bạn có thể dùng `go install github.com/TuanPhanDuy/dispatch/cmd/dispatch@latest`.

### Agent

dispatch chỉ khởi chạy các CLI agent, không cài chúng. Hãy tự cài những agent bạn cần (ví dụ Claude Code hoặc Codex) sao cho gõ tên của chúng trong terminal là chạy được. Ứng dụng desktop đọc `PATH` từ login shell của bạn, nên terminal tìm thấy lệnh nào thì ứng dụng cũng tìm thấy lệnh đó.

<a id="desktop-app"></a>
## Ứng dụng desktop

### Danh sách session

Đây là màn hình đầu tiên. Mỗi dòng hiện tên session, trạng thái, lệnh agent và thư mục làm việc.

- **+ New session** (`⌘N`): đặt tên, chọn agent và thư mục làm việc.
- **Attach** mở session trong một pane terminal. **Restart**, **Kill**, **Rename** và **Remove** (với session đã thoát) nằm cùng dòng.
- **Flat / By project** nhóm các session theo thư mục làm việc.
- Dải phía trên danh sách là **các lần khởi chạy gần đây**. Bấm vào một mục để chạy lại đúng agent đó trong đúng thư mục đó.
- **Jump to session** (`⌘K`) tìm session và lần khởi chạy gần đây theo tên hoặc đường dẫn.

Khi một session chuyển từ đang chạy sang *needs attention*, bạn nhận được thông báo của hệ thống và một thông báo nhỏ trong ứng dụng; bấm vào thông báo nào cũng sẽ mở session đó. Biểu tượng chuông và badge trên Dock cho biết có bao nhiêu session đang chờ bạn.

### Tab và pane

Mỗi session bạn attach sẽ thành một **tab**. Các tab vẫn giữ kết nối khi chạy nền, nên chuyển tab là tức thì và không phải tải lại scrollback.

Vùng bên dưới các tab có thể chia thành nhiều **pane** để bạn theo dõi nhiều agent cạnh nhau:

1. Nhấn `⌘D` để chia pane đang focus sang phải, hoặc `⌘⇧D` để chia xuống dưới. Bạn cũng có thể dùng nút chia trên thanh trên cùng hoặc trên đầu mỗi pane.
2. Chọn session sẽ nằm trong pane mới: một session có sẵn, một lần khởi chạy gần đây, hoặc **New session…**.
3. Kéo đường ranh giới giữa hai pane để đổi kích thước. Terminal của mỗi agent tự co giãn theo pane, nên các agent dùng toàn màn hình vẫn hiển thị đúng.

Thao tác với pane:

| Muốn làm | Cách làm |
|---|---|
| Focus sang pane khác | Bấm vào pane đó, hoặc `⌘⌥` + phím mũi tên |
| Phóng to một pane ra cả cửa sổ, rồi thu lại | `⌘⇧↵`, hoặc biểu tượng phóng to trên đầu pane |
| Đưa một tab đang chạy nền vào pane đang focus | Bấm vào tab đó |
| Mở một tab chạy nền cạnh pane đang focus | Biểu tượng chia trên tab đó |
| Đóng pane nhưng giữ session ở dạng tab | `⌘⇧W`, hoặc dấu × trên đầu pane |
| Detach hẳn một tab | `⌘W`, hoặc dấu × trên tab |

Pane đang focus có viền màu. Pane có agent cần chú ý mà không được focus sẽ nhấp nháy viền cam cho tới khi bạn xem nó.

Bố cục pane được lưu lại. Lần sau mở ứng dụng, các session cũ mở lại đúng vị trí cũ; session nào đã thoát trong lúc đó sẽ được bỏ qua.

### Phím tắt

| Phím tắt | Tác dụng |
|---|---|
| `⌘K` | Chuyển nhanh tới một session |
| `⌘N` | Tạo session mới |
| `⌘1` – `⌘9` | Chuyển tới tab 1–9 |
| `⌘]` / `⌘[` | Tab kế tiếp / tab trước |
| `⌘D` / `⌘⇧D` | Chia sang phải / chia xuống dưới |
| `⌘⌥` + mũi tên | Di chuyển giữa các pane |
| `⌘⇧↵` | Phóng to / thu nhỏ pane |
| `⌘⇧W` | Đóng pane (giữ lại tab) |
| `⌘W` | Detach tab hiện tại |
| `⌘/` | Hiện danh sách phím tắt |

Mọi phím khác bạn gõ đều được gửi thẳng tới agent trong pane đang focus, kể cả `ctrl+C` và `ctrl+D`.

<a id="tui"></a>
## Giao diện terminal (TUI)

Chạy:

```sh
dispatch
```

### Danh sách session

| Phím | Tác dụng |
|---|---|
| `↑` / `↓`, `j` / `k` | Di chuyển lựa chọn |
| `enter` | Attach vào session đang chạy, hoặc khởi chạy một session đã lưu |
| `n` | Tạo session mới |
| `r` | Đổi tên |
| `R` | Khởi động lại |
| `d` | Kill session đang chạy, hoặc xoá session đã thoát/đã lưu (có hỏi xác nhận) |
| `?` | Trợ giúp |
| `q`, `ctrl+C` | Thoát TUI — các session vẫn tiếp tục chạy |

**New session** là một wizard ngắn: chọn profile (xem [Cấu hình](#configuration)), nhập tên, xác nhận thư mục làm việc. `enter` để sang bước tiếp theo, `esc` để huỷ.

### Bên trong một session

Mọi phím bạn gõ đều được gửi tới agent. Phím duy nhất được dành riêng là `ctrl+\`, dùng để mở menu lệnh một lần:

| `ctrl+\` rồi nhấn | Tác dụng |
|---|---|
| `d` | Detach và quay về danh sách (session vẫn chạy) |
| `k` | Kill session |
| `q` | Thoát TUI |
| `esc` | Huỷ |

TUI chỉ hiện một session tại một thời điểm. Dùng ứng dụng desktop nếu bạn muốn xem nhiều session cạnh nhau.

<a id="multiple-claude-accounts"></a>
## Nhiều account Claude

Mỗi session Claude Code có thể chạy dưới một **account** riêng, nhờ đó bạn đăng nhập nhiều account Claude cùng lúc — ví dụ gói Pro cá nhân ở pane này và gói Team của công ty ở pane bên cạnh.

Mỗi account là một "nhà" Claude Code riêng: login, settings, lịch sử hội thoại, MCP server và `CLAUDE.md` cấp người dùng riêng — tức là mọi thứ bình thường nằm trong `~/.claude`. File của project, như thư mục `.claude/` trong repo, vẫn dùng chung vì chúng thuộc về project. Session nào không chọn account thì vẫn dùng `~/.claude` như cũ.

> Hãy dùng account để tách những danh tính thật sự khác nhau, như cá nhân và công việc. Chia công việc của một người ra nhiều account để lách giới hạn sử dụng của gói có thể vi phạm điều khoản của Anthropic; hãy tự kiểm tra cho trường hợp của bạn.

### Trong ứng dụng desktop

1. Bấm **+ New session** và chọn agent **Claude Code**. Ô **Claude account** sẽ xuất hiện.
2. Chọn một trong:
   - **Default (~/.claude)** — login bạn vẫn dùng.
   - **@tên** — một account bạn đã dùng trước đó.
   - **+ New account…** — nhập tên, ví dụ `work` hoặc `personal` (chữ, số, `.`, `_`, `-`).
3. Bấm **Create session**.

Session đầu tiên của một account mới khởi động giống Claude Code vừa cài: chọn theme rồi đăng nhập. Claude Code sẽ mở trình duyệt để đăng nhập — hãy chắc trình duyệt đang đăng nhập đúng account Claude bạn muốn dùng cho account này, ví dụ dùng một profile trình duyệt riêng, hoặc đăng xuất khỏi claude.ai trước. Nếu đang ở trong session mà cần đăng nhập lại, gõ `/login`.

Từ đó trở đi, mọi session mới dưới cùng account đều đã được đăng nhập sẵn. Hộp thoại nhớ account bạn chọn lần gần nhất.

Session dùng account có chip **@tên** trong danh sách session, trên tab và trên đầu pane, nên bạn luôn biết pane nào đang dùng account nào. **Restart** giữ nguyên account của session, và dải khởi chạy gần đây cũng chạy lại đúng account đó.

### Trong giao diện terminal

Thêm vào [config.yaml](#configuration) một profile có `account` cho mỗi account bạn cần:

```yaml
profiles:
  - name: claude-work
    command: claude
    account: work
  - name: claude-personal
    command: claude
    account: personal
```

Chọn profile đó trong wizard **New session**. Danh sách session hiện account bên cạnh trạng thái của từng session. Ứng dụng desktop và TUI dùng chung account: profile có `account: work` dùng cùng login với **@work** trong ứng dụng desktop.

<a id="where-accounts-live"></a>
### Account được lưu ở đâu

| Hệ điều hành | Thư mục |
|---|---|
| macOS | `~/Library/Application Support/dispatch/accounts/<tên>` |
| Linux | `~/.local/share/dispatch/accounts/<tên>` (hoặc `$XDG_DATA_HOME/dispatch/accounts/<tên>`) |

dispatch chạy session với `CLAUDE_CONFIG_DIR` trỏ tới thư mục đó. Trên macOS, Claude Code lưu token đăng nhập của mỗi account vào Keychain dưới một mục riêng.

### Xoá một account

1. Mở một session dưới account đó và gõ `/logout` để xoá login khỏi Keychain.
2. Xoá thư mục của nó, ví dụ `rm -rf ~/Library/Application\ Support/dispatch/accounts/work`.

<a id="configuration"></a>
## Cấu hình

Giao diện terminal đọc file `config.yaml` và tự tạo file này với giá trị mặc định ở lần chạy đầu:

- macOS: `~/Library/Application Support/dispatch/config.yaml`
- Linux: `~/.config/dispatch/config.yaml` (hoặc `$XDG_CONFIG_HOME/dispatch/config.yaml`)

```yaml
defaults:
  scrollback_lines: 5000        # số dòng lịch sử giữ lại cho mỗi session
  idle_attention_seconds: 10    # thời gian im lặng trước khi báo "needs attention"

profiles:                       # các lựa chọn trong wizard New session của TUI
  - name: claude
    command: claude
    idle_attention_seconds: 15  # ghi đè riêng cho profile này
  - name: codex
    command: codex
    args: ["--full-auto"]
  - name: shell
    command: zsh
  - name: claude-work
    command: claude
    account: work               # chạy dưới một account Claude riêng

sessions: []                    # dispatch tự ghi; xem bên dưới
```

- **profiles** là các mẫu. Bạn có thể thêm profile cho bất kỳ CLI nào, kèm tham số. `account` cho session của profile chạy dưới một [account Claude riêng](#multiple-claude-accounts).
- **sessions** do dispatch tự quản lý: mỗi session bạn tạo hoặc đổi tên được lưu với tên, profile và thư mục, nên nó vẫn còn trong danh sách sau khi thoát và có thể khởi chạy lại bằng `enter`.

Ứng dụng desktop không dùng profiles. Hộp thoại **New session** của nó đưa ra Claude Code, Codex và Aider nếu chúng đã được cài, cùng với login shell của bạn, và nó giữ danh sách khởi chạy gần đây riêng.

Kích thước scrollback và thời gian chờ mặc định được chốt khi daemon khởi động, theo giao diện nào khởi động daemon trước. Thay đổi chỉ có hiệu lực sau khi khởi động lại daemon.

<a id="updating"></a>
## Cập nhật

1. Cài phiên bản mới theo cùng cách bạn đã cài phiên bản cũ.
2. Khởi động lại daemon để nó chạy code mới. **Việc này kết thúc mọi session đang chạy**, nên hãy để các agent làm xong hoặc lưu công việc trước:

   ```sh
   pkill -f "dispatch daemon"
   ```

3. Mở lại dispatch; nó sẽ khởi động một daemon mới.

Nếu chưa khởi động lại, daemon cũ vẫn chạy phiên bản cũ, dù giao diện đã là bản mới.

<a id="troubleshooting"></a>
## Xử lý sự cố

**"dispatch can't be opened because Apple cannot check it for malicious software."**
Ứng dụng chưa được notarize. Chuột phải → **Open**, hoặc chạy `xattr -dr com.apple.quarantine /Applications/dispatch.app`.

**Hộp thoại New session của ứng dụng desktop không có agent mình cần.**
Ứng dụng chỉ liệt kê những agent tìm thấy trong `PATH` của login shell. Kiểm tra `which claude` (hoặc `codex`, `aider`) chạy được trong một cửa sổ terminal mới, rồi mở lại ứng dụng.

**Session thoát ngay với lỗi "command not found".**
Daemon không tìm thấy lệnh. Nếu daemon được khởi động từ một môi trường có `PATH` khác, hãy khởi động lại nó từ môi trường bình thường của bạn (xem [Cập nhật](#updating)).

**"Could not connect to daemon".**
dispatch kết nối tới daemon qua socket tại `$XDG_RUNTIME_DIR/dispatch.sock` nếu biến này được đặt, nếu không thì tại `$TMPDIR/dispatch-<uid của bạn>/dispatch.sock`. Nếu daemon bị crash và để lại socket cũ, hãy dừng daemon còn sót bằng `pkill -f "dispatch daemon"` rồi mở lại dispatch.

**Trong ứng dụng desktop, mở nhiều tab nhưng chỉ một tab cập nhật.**
Daemon của bạn cũ hơn v0.2.0, khi đó mỗi cửa sổ chỉ nhận output của một session. Hãy khởi động lại daemon (xem [Cập nhật](#updating)).

**Ô Claude account bị mờ và nhắc khởi động lại daemon.**
Daemon của bạn cũ hơn bản hỗ trợ account (v0.4.0). Hãy khởi động lại nó (xem [Cập nhật](#updating)). Trước khi khởi động lại, session sẽ chạy bằng login mặc định.

**Session dưới một account lần nào cũng mở ra màn hình chào / đăng nhập.**
Account đó chưa được đăng nhập: làm xong bước chọn theme và đăng nhập một lần, các session sau dưới account đó sẽ dùng lại. Nếu vẫn bị hỏi mãi, kiểm tra thư mục của account còn tồn tại không (xem [Account được lưu ở đâu](#where-accounts-live)).

**Báo "Needs attention" trong khi agent vẫn đang làm.**
Một số agent suy nghĩ khá lâu mà không in gì. Hãy tăng `idle_attention_seconds` cho profile đó.

**Tất cả session biến mất.**
Daemon đã bị dừng: do khởi động lại, crash, đăng xuất hoặc khởi động lại máy. Session không tồn tại lâu hơn daemon. Trong TUI, các session đã lưu vẫn còn trong danh sách và có thể chạy lại bằng `enter`; trong ứng dụng desktop, dùng dải khởi chạy gần đây.

<a id="uninstalling"></a>
## Gỡ cài đặt

```sh
pkill -f "dispatch daemon"                          # kết thúc mọi session
rm -rf /Applications/dispatch.app                   # ứng dụng desktop
sudo rm -f /usr/local/bin/dispatch                  # giao diện terminal
rm -rf ~/Library/Application\ Support/dispatch      # cài đặt, khởi chạy gần đây, bố cục pane, account trên macOS
rm -rf ~/.config/dispatch ~/.local/share/dispatch   # cài đặt và account trên Linux
```

Nếu bạn có dùng [account](#multiple-claude-accounts), hãy gõ `/logout` trong một session của từng account trước, để xoá login của chúng khỏi Keychain.
