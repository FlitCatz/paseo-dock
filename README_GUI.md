# Paseo GUI에서 사용하기

이 문서는 paseo-sbx로 만든 sandbox 조합을 Paseo 앱에서 사용하는 방법을 설명합니다.
설치와 CLI 옵션 전체는 [README.md](README.md)를 참고하세요.

Paseo 화면의 메뉴 이름과 위치는 Paseo 버전에 따라 다를 수 있습니다. 이 문서는 Paseo 0.10.1을
기준으로 작성했습니다.

## 동작 방식

Paseo 모델 선택 목록에는 조합 이름이 `SBX codex-superpowers`처럼 **한 번씩만** 표시됩니다.
어느 프로젝트의 sandbox로 연결할지는 목록이 아니라 **세션의 프로젝트 폴더**로 정해집니다.

예를 들어 프로젝트 5개를 다음처럼 구성했다고 가정합니다.

| 프로젝트 | 사용할 조합 |
| --- | --- |
| A, B, C | codex-superpowers, claude-superpowers |
| D | codex-gsd-core, claude-gsd-core |
| E | codex-ponytail, claude-ponytail |

- Paseo 목록에는 조합 여섯 개가 표시됩니다.
- A 폴더에서 `SBX codex-superpowers`를 고르면 A의 sandbox에서 실행됩니다.
  B 폴더에서 같은 항목을 고르면 B의 sandbox에서 실행됩니다.
- 각 sandbox는 자기 프로젝트 폴더만 볼 수 있습니다. A의 agent는 B의 파일에 접근할 수 없습니다.
- 세션마다 별도 프로세스로 실행되므로 A와 B를 동시에 사용할 수 있습니다.
- 목록의 모든 항목이 모든 프로젝트에서 동작하지는 않습니다. A에서 `SBX codex-gsd-core`를
  고르면 A에 gsd-core 조합이 없으므로 실행이 거부됩니다.

## 1. 프로젝트 준비 (프로젝트마다 한 번, 터미널)

paseo-sbx 저장소에서 프로젝트마다 `create`를 실행합니다. `--workspace`에는 **프로젝트
폴더의 절대 경로**를 지정합니다. paseo-sbx 저장소 폴더나 그 상위 폴더는 지정할 수 없습니다.

```bash
cd /path/to/paseo-sbx
make build

./paseo-sbx create --workspace /path/to/A --profiles codex-superpowers,claude-superpowers --build --apply
./paseo-sbx create --workspace /path/to/B --profiles codex-superpowers,claude-superpowers --apply
./paseo-sbx create --workspace /path/to/C --profiles codex-superpowers,claude-superpowers --apply
./paseo-sbx create --workspace /path/to/D --profiles codex-gsd-core,claude-gsd-core --build --apply
./paseo-sbx create --workspace /path/to/E --profiles codex-ponytail,claude-ponytail --build --apply
```

- 같은 조합과 utilities를 쓰는 이미지는 한 번만 빌드하면 됩니다. 위 예에서 B와 C는 A에서
  빌드한 이미지를 재사용하므로 `--build`를 생략했습니다.
- utilities(RTK, Headroom)를 쓰려면 `--utilities rtk,headroom`을 추가합니다. utilities가
  다르면 이미지가 달라지므로 그 조합으로 처음 만들 때 `--build`를 붙입니다.
  Antigravity에서는 RTK를 선택할 수 있습니다. Headroom 연동은 아직 구현하지 않았습니다.
- `--profiles` 없이 터미널에서 `create --workspace <path>`를 실행하면 선택 화면(TUI)이 열립니다.
  Agent·방법론·utility 목록이 함께 표시되고 현재 그룹을 강조합니다. ↑/↓로 이동,
  Space/x로 체크, Enter/Tab으로 다음 그룹, Esc/Shift+Tab으로 이전 그룹을 선택합니다.
  작은 터미널에서는 현재 항목을 따라 스크롤합니다. `NO_COLOR=1`로 색상을 끌 수 있습니다.
  마지막 화면에서 **Paseo에 선택한 provider 등록 및 reload**를 체크하면 `--apply`와 같습니다.
  최종 확인은 Enter로 확정합니다. OpenSpec도 세 에이전트의 방법론 목록에서 선택할 수 있으며,
  선택하면 workspace에 `openspec/` 및 에이전트별 스킬·workflow 파일을 초기화합니다.
- `--apply`는 Paseo 설정을 백업한 뒤 provider를 등록하고 Paseo를 reload합니다.

등록 결과는 다음 명령으로 확인합니다.

```bash
./paseo-sbx list --all
```

## 2. 모델 로그인 (sandbox마다 한 번)

sandbox는 프로젝트와 조합마다 따로 있으므로 로그인도 각각 필요합니다.

- **Codex:** `sbx secret set openai --oauth`로 한 번 등록하면 모든 Codex sandbox에 적용됩니다.
- **Claude API key:** `sbx secret set anthropic`으로 한 번 등록하면 모든 Claude sandbox에 적용됩니다.
- **Antigravity CLI:** `antigravity-cli-native`, `antigravity-cli-superpowers`,
  `antigravity-cli-gsd-core`, `antigravity-cli-ponytail`, `antigravity-cli-openspec` 중 필요한 조합을 생성하고,
  각 sandbox shell에서 공식 `agy`로 로그인합니다. `paseo-sbx run --workspace … --profile …`으로
  기존 플러그인에 SBX 에이전트를 생성한 뒤 앱에서 사용합니다. 별도 SBX 공급자 항목은
  추가되지 않습니다. [Antigravity 인증·실행 절차](README.md#antigravity-cli)를 참고하세요.
- **Claude 구독 로그인:** Claude sandbox마다 로그인합니다.

  ```bash
  ./paseo-sbx shell claude-superpowers --workspace /path/to/A
  # sandbox 안에서
  claude        # /login 진행 후 종료
  ```

  B, C, D, E의 Claude sandbox에서도 같은 방식으로 로그인합니다.

모델 목록 조회는 해당 조합의 sandbox 중 경로 순으로 첫 번째 sandbox에서 실행됩니다.
로그인하지 않은 sandbox가 선택되면 모델 목록이 비거나 오류가 날 수 있으므로 모두 로그인해 두세요.

## 3. GUI에서 세션 시작

1. Paseo 앱을 엽니다. 등록 직후 목록이 보이지 않으면 앱을 다시 열거나 `paseo daemon restart`를 실행합니다.
2. 새 agent(세션)를 만들 때 **작업 폴더를 프로젝트 폴더 또는 그 하위 폴더**로 지정합니다.
3. 모델 선택 목록에서 그 프로젝트에 만든 조합(`SBX codex-superpowers` 등)을 선택하고 모델을 고릅니다.
4. 메시지를 보내면 해당 프로젝트의 sandbox 안에서 agent가 실행됩니다.

다른 프로젝트도 같은 방식으로 세션을 열면 됩니다. A와 B에서 동시에 작업해도 서로 영향을 주지 않습니다.

### 프로젝트별 선택 요약

| 작업 폴더 | 선택할 항목 |
| --- | --- |
| A, B, C | `SBX codex-superpowers`, `SBX claude-superpowers` |
| D | `SBX codex-gsd-core`, `SBX claude-gsd-core` |
| E | `SBX codex-ponytail`, `SBX claude-ponytail` |

## 4. 하위 agent 위임

sandbox 안의 agent는 Paseo 도구로 하위 agent를 만들 수 있습니다. 예를 들어 A의
`codex-superpowers` agent가 검토를 `claude-superpowers`에 맡기면, 하위 agent도 A 폴더의
A sandbox에서 실행됩니다.

- 위임할 수 있는 조합은 **호출한 프로젝트에 등록된 조합**뿐입니다. A의 agent는
  `claude-gsd-core`(D의 조합)로 위임할 수 없습니다.
- 다른 폴더를 지정한 위임이나 터미널·스케줄 등 호스트 도구 사용은 거부됩니다.

## 5. 프로젝트 추가·변경·제외

| 하고 싶은 일 | 명령 |
| --- | --- |
| 프로젝트 추가 | `./paseo-sbx create --workspace /path/to/F --profiles ... --apply` |
| 프로젝트를 목록 연결에서 제외 | `./paseo-sbx unregister --workspace /path/to/B` |
| 제외한 프로젝트 다시 사용 | `./paseo-sbx create --workspace /path/to/B --skip-build --apply` |
| 등록 상태 확인 | `./paseo-sbx list --all` |
| 삭제한 프로젝트 정리 | `./paseo-sbx prune --dry-run`으로 확인 후 `./paseo-sbx prune` |
| paseo-sbx 업데이트 후 반영 | `make build` 후 `./paseo-sbx register --workspace <아무 등록 프로젝트> --apply` |

- `unregister`는 sandbox를 삭제하지 않습니다. 그 프로젝트에서만 쓰던 조합은 Paseo 목록에서 사라집니다.
- 다시 사용할 때 터미널에서는 저장된 조합이 체크된 선택 화면이 열립니다. Enter로 확정하면 됩니다.
  `--skip-build`는 이미 로드된 이미지를 그대로 쓰게 합니다.
- 프로젝트 폴더를 삭제하면 `list --all`에 `"missing": true`로 표시되고, 연결과 Paseo 목록에서
  자동으로 제외됩니다. `prune`은 남은 기록만 지우고 sandbox는 삭제하지 않습니다.
  sandbox까지 지우려면 `prune`이 안내하는 `sbx rm` 명령을 직접 실행합니다(되돌릴 수 없음).
- 이미 만든 프로젝트의 조합을 바꾸면 기존 sandbox와 설정이 달라 `create`가 재사용을 거부할 수 있습니다.
  자세한 처리 방법은 README의 "유틸리티와 확장" 절을 참고하세요.

## 6. 문제 해결

| 증상 | 원인 | 해결 |
| --- | --- | --- |
| `Paseo cwd is outside every registered sandbox workspace for <조합>` | 작업 폴더가 그 조합을 등록한 프로젝트에 속하지 않음 | 작업 폴더를 확인하거나 그 프로젝트에 등록된 조합을 선택. 필요하면 `create`로 조합 추가 |
| 목록에 `SBX ...` 항목이 없음 | 등록 전이거나 Paseo가 이전 상태를 캐시 | `./paseo-sbx register --workspace <path> --apply` 후 `paseo daemon restart` |
| 모델 목록이 비어 있거나 조회 오류 | 조회에 쓰인 sandbox가 로그인되지 않음 | 해당 조합의 모든 sandbox에서 로그인 |
| 세션 시작 후 인증 오류 | 그 프로젝트의 sandbox가 로그인되지 않음 | `./paseo-sbx shell <조합> --workspace <path>`로 로그인 |
| `workspace ... must not contain ...` | 프로젝트 폴더가 paseo-sbx 소스 또는 `~/.local/share/paseo-sbx`를 포함 | 실제 프로젝트 폴더를 `--workspace`로 지정 |

상세 진단 명령:

```bash
paseo provider ls
paseo provider diagnostic codex-superpowers
sbx ls
./paseo-sbx list --all
```

## 주의

sandbox는 프로젝트 폴더에 쓸 수 있으므로 agent가 `.git/hooks`, `Makefile`,
`package.json` scripts 등을 바꿀 수 있습니다. 호스트에서 `git`, `make`, `npm` 등을
실행하기 전에 변경 내용을 확인하세요.
