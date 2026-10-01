@~/.codex/RTK.md

## 구현 언어

- 이 codebase의 구현 언어는 Go(Golang)다. 새 기능, CLI, helper 및 테스트는 Go로 작성한다.
- 편의를 위해 Python 등 다른 언어의 구현이나 런타임 의존성을 임의로 추가하지 않는다.
- 기존 Dockerfile·Shell·CI 설정은 유지·수정할 수 있지만, 새 실행 로직은 Go로 구현한다.
- 다른 언어가 꼭 필요한 경우 이유와 범위를 설명하고 사용자의 명시적 동의를 받은 뒤 진행한다.

## 설계 및 계획 검토

- 검토가 필요한 설계·계획은 파일 링크뿐 아니라 본문도 채팅에 제시한다.
- 파일은 검토 기록으로 함께 제공한다. 링크만으로 설명을 대신하지 않는다.
- 사용자가 설계에 동의하고 작업 시작을 지시하면 같은 내용의 승인을 반복해서 요청하지 않는다.

<!-- CODEGRAPH_START -->
## CodeGraph

In repositories indexed by CodeGraph (a `.codegraph/` directory exists at the repo root), reach for it BEFORE grep/find or reading files when you need to understand or locate code:

- **MCP tool** (when available): `codegraph_explore` answers most code questions in one call — the relevant symbols' verbatim source plus the call paths between them, including dynamic-dispatch hops grep can't follow. Name a file or symbol in the query to read its current line-numbered source. If it's listed but deferred, load it by name via tool search.
- **Shell** (always works): `codegraph explore "<symbol names or question>"` prints the same output.

If there is no `.codegraph/` directory, skip CodeGraph entirely — indexing is the user's decision.
<!-- CODEGRAPH_END -->
