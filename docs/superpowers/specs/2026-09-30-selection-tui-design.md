# Agent·Methodology 선택 TUI 및 유틸리티 설치 제안

상태: 사용자가 채팅에 제시된 조합별 sandbox 설계에 대해 `AGENTS.md 에 명시하고 작업 시작해`라고 지시하여 구현을 승인했다. 구현 및 검증 기록은 구현 계획과 저장소의 구현 기록에 남긴다.

## 목표

기존 `create`는 Codex·Claude와 Superpowers·GSD Core·Ponytail의 조합 여섯 개를 모두 생성했다. 변경 후에는 사용자가 선택한 조합만 빌드·생성·Paseo provider 등록에 사용한다. 선택한 유틸리티는 해당 sandbox에 설치하고 agent에서 사용할 수 있도록 설정한다.

호스트 관리 CLI와 launcher는 Go로 유지한다. 향후 agent, methodology, utility를 추가할 수 있도록 목록과 실행 방식을 분리한다.

## 구성 방식

기존의 조합별 sandbox 구조를 유지한다. Codex와 Superpowers·Ponytail을 선택하면 `codex-superpowers`, `codex-ponytail` 두 개가 생긴다. methodology별 설정과 실행 환경을 분리하고 기존 Paseo provider 구조를 활용한다.

## 사용자 흐름

`create`를 터미널에서 실행하고 선택 옵션이 없으면 Charmbracelet Bubble Tea 기반 선택 화면을 연다.

1. 작업 workspace를 확인한다.
2. agent를 하나 이상 선택한다.
3. 각 agent에 사용할 methodology를 복수 선택한다. 지원하지 않는 항목은 선택할 수 없다.
4. RTK·Headroom 등의 utility를 복수 선택한다. 기본값은 선택 없음이다.
5. 실제 생성할 조합, 유틸리티, 자원 설정을 확인한다.
6. 확인된 선택으로 필요한 이미지 빌드·로드와 sandbox 생성을 실행한다. 호스트 launcher와 선택 사본 저장을 마친 뒤 활성 선택을 registry에 원자적으로 확정한다. Paseo 등록은 기존 `register --apply` 의미를 유지하며 TUI에서 명시적으로 선택한 경우 함께 실행한다.

방향키로 이동하고 Space로 선택, Enter로 다음 단계, Esc로 이전 단계, Ctrl+C로 취소한다. 진행 화면은 현재 작업과 실패 원인을 표시한다. 선택 단계에서 취소하면 sandbox나 Paseo 설정을 변경하지 않는다.

비대화형 실행을 위해 agent·methodology 조합과 utilities를 직접 지정하는 CLI 옵션 및 저장된 선택 읽기를 제공한다. TTY가 없고 선택도 없으면 전체 여섯 개를 생성하지 않고 필요한 옵션을 안내한다.

## 선택과 카탈로그

workspace별 신뢰한 host runtime의 registry에 활성 선택과 대응하는 catalog를 함께 저장한다. `selection.json`은 가져오기용 사본이다. registry의 확정된 선택을 `build`, `load`, `create`, `register`가 사용한다. `list`, `shell`, `stop`은 실제 registry 항목을 기준으로 동작한다.

카탈로그에는 표시명, ID, 지원 agent, 설치 방식과 필요한 설정을 정의한다. 기본은 `internal/config/catalog.json`이며 source root의 `catalog.json`으로 대체할 수 있다. 버전·revision·base image digest·다운로드 checksum은 lock 파일에 고정한다. 여섯 개 조합 하드코딩과 base image 개수 제한을 제거한다.

새 methodology·utility는 지원하는 설치 방식이면 카탈로그와 lock 항목 추가로 확장한다. 새 agent는 설치뿐 아니라 Paseo 연결과 CLI 프로토콜 처리를 위한 adapter가 필요할 수 있다. 미지원 adapter를 기존 Codex·Claude처럼 실행하지 않는다.

## 유틸리티

RTK는 아키텍처에 맞는 고정 버전 바이너리를 sandbox에 설치하고 Codex·Claude별 공식 초기화 방식을 적용한다. 설치 여부와 agent 연동 여부를 각각 확인한다. 기존 instruction과 hook 설정을 보존해 병합한다.

Headroom의 공식 CLI·MCP 서버에는 Python 런타임이 필요하다. 선택한 sandbox 내부에만 전용 런타임과 고정 버전 패키지를 설치한다. 초기 제안은 공식 stdio MCP 서버를 agent에 등록해 압축·조회 도구를 제공하는 방식이다. 모델 요청 전체에 대한 자동 압축을 의미하지 않는다. 프록시 모드는 현재 sbx 인증·네트워크·Paseo 호출 방식과 별도 검증한 뒤 추가한다. Node SDK만 설치해서 CLI 설치로 표시하지 않는다.

선택한 utilities는 선택된 모든 조합에 적용한다. 유틸리티 선택 화면에서 Headroom의 추가 런타임 의존성을 안내한다. 유틸리티를 선택하지 않은 sandbox에는 설치하지 않는다.

## 기존 환경 및 오류 처리

선택에서 제외된 관리 대상 provider는 활성 매핑에서 제외한다. 실제 기존 sandbox와 관련 없는 Paseo provider는 보존한다. 선택된 provider만 delegation allowlist에 넣는다.

agent·methodology·utility 버전과 선택 내용을 이미지 식별자에 포함한다. 기존 sandbox 이미지가 새로운 선택과 다르면 재사용 성공으로 처리하지 않고 차이를 표시한다. 실행 중 sandbox를 임의로 삭제·재생성하지 않는다.

빌드·설치 실패 시 다음 단계로 진행하지 않는다. 부분 생성된 sandbox 정보는 registry에 보존하고 재실행 시 확인된 항목을 재사용한다. Paseo 설정 변경 전 백업을 유지한다.

## 검증

선택하지 않은 조합이 이미지 대상·생성 명령·provider·allowlist에 들어가지 않는지 테스트한다. TUI 선택·취소, 비대화형 실행, 호환성 검증, utility 변경에 따른 이미지 차이, 기존 registry 처리와 부분 실패를 검증한다. 실제 Codex·Claude sandbox에서 RTK 실행·연동과 Headroom MCP 연결을 확인한다.

## 근거

- Bubble Tea: https://github.com/charmbracelet/bubbletea
- RTK 설치 및 agent 통합: https://github.com/rtk-ai/rtk
- Headroom 설치 및 CLI·SDK 구분: https://github.com/headroomlabs-ai/headroom
- Headroom 공식 MCP 실행 정의: https://github.com/headroomlabs-ai/headroom/blob/main/server.json
