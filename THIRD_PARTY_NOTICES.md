# Third-party notices

`rukia-player` includes and links third-party Go modules. This document
preserves attribution and records the licenses verified for the **direct**
dependencies declared in `go.mod` at the time of this change. It does not
replace a complete license review of all transitive dependencies or the
corresponding-source obligations applicable to distributed binaries.

## Direct dependencies

| Component | Version | License | Attribution / license source |
| --- | --- | --- | --- |
| [go-librespot](https://github.com/devgianlu/go-librespot) | v0.10.2 | GNU GPL version 3 | [Upstream LICENSE](https://github.com/devgianlu/go-librespot/blob/v0.10.2/LICENSE) |
| [Bubbles](https://github.com/charmbracelet/bubbles) | v1.0.0 | MIT | Copyright (c) 2020-2025 Charmbracelet, Inc. [Upstream LICENSE](https://github.com/charmbracelet/bubbles/blob/v1.0.0/LICENSE) |
| [Bubble Tea](https://github.com/charmbracelet/bubbletea) | v1.3.10 | MIT | Copyright (c) 2020-2025 Charmbracelet, Inc. [Upstream LICENSE](https://github.com/charmbracelet/bubbletea/blob/v1.3.10/LICENSE) |
| [Lip Gloss](https://github.com/charmbracelet/lipgloss) | v1.1.0 | MIT | Copyright (c) 2021-2023 Charmbracelet, Inc. [Upstream LICENSE](https://github.com/charmbracelet/lipgloss/blob/v1.1.0/LICENSE) |
| [golang.org/x/oauth2](https://github.com/golang/oauth2) | v0.37.0 | BSD-3-Clause | Copyright 2009 The Go Authors. [Upstream LICENSE](https://github.com/golang/oauth2/blob/v0.37.0/LICENSE) |

The GPLv3 license text is available in `LICENSE` and
`LICENSES/GPL-3.0-only.txt`. The MIT license text appears in
`LICENSES/MIT.txt`, along with the copyright notice for the original
`rukia-player` code; the separate MIT copyright notices above apply to
the listed Charmbracelet modules.

### BSD-3-Clause notice for `golang.org/x/oauth2`

Copyright 2009 The Go Authors.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google LLC nor the names of its
contributors may be used to endorse or promote products derived from
this software without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
OWNER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

## Redistributing binaries

When publishing binaries, ship the applicable license and copyright notices
with the distribution. Review the complete resolved module graph (including
transitive modules), bundled assets, and native libraries, and satisfy any
additional attribution, license-text, and source-provision requirements.
`go.mod` and `go.sum` identify the dependency versions but are not themselves
a substitute for the required notices or complete corresponding source.
