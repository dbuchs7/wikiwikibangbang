# Third-party notices

wikiwikibangbang is licensed under AGPL-3.0-only (see `LICENSE`). The binary includes the following third-party modules. Their licenses are compatible with AGPL-3.0 (Apache-2.0, BSD-3-Clause and MIT may be combined with GPLv3-family licenses). `internal/repoconfig` checks that this file names every module in `go.mod`.

| Module | Version | License |
|---|---|---|
| github.com/beorn7/perks | v1.0.1 | MIT |
| github.com/cespare/xxhash/v2 | v2.3.0 | MIT |
| github.com/coreos/go-oidc/v3 | v3.21.0 | Apache-2.0 |
| github.com/go-jose/go-jose/v4 | v4.1.4 | Apache-2.0 |
| github.com/munnerz/goautoneg | v0.0.0-20191010083416-a7dc8b61c822 | BSD-3-Clause |
| github.com/prometheus/client_golang | v1.24.1 | Apache-2.0 |
| github.com/prometheus/client_model | v0.6.2 | Apache-2.0 |
| github.com/prometheus/common | v0.70.1 | Apache-2.0 |
| github.com/prometheus/procfs | v0.21.1 | Apache-2.0 |
| golang.org/x/crypto | v0.58.0 | BSD-3-Clause |
| golang.org/x/oauth2 | v0.37.0 | BSD-3-Clause |
| golang.org/x/sys | v0.49.0 | BSD-3-Clause |
| google.golang.org/protobuf | v1.36.11 | BSD-3-Clause |

The web UI embeds a bundle of these JavaScript packages, built from `internal/web/vendor` (ADR-0032); `internal/web/static/vendor/prosemirror.LICENSE.txt` ships next to it:

| Package | Version | License |
|---|---|---|
| orderedmap | 2.1.1 | MIT |
| prosemirror-commands | 1.7.2 | MIT |
| prosemirror-history | 1.5.1 | MIT |
| prosemirror-inputrules | 1.5.1 | MIT |
| prosemirror-keymap | 1.2.3 | MIT |
| prosemirror-model | 1.25.12 | MIT |
| prosemirror-state | 1.4.4 | MIT |
| prosemirror-transform | 1.12.2 | MIT |
| prosemirror-view | 1.42.6 | MIT |
| rope-sequence | 1.3.4 | MIT |
| w3c-keyname | 2.2.8 | MIT |

The drawing editor (ADR-0053, ADR-0126) is a second bundle, `internal/web/static/vendor/excalidraw.js` with its stylesheet `excalidraw.css`, built from the same directory, with the chunks it loads when needed in `excalidraw-assets/` (among them the German translation) and its fonts in `fonts/`. Excalidraw's import of Mermaid diagrams is left out of it. `excalidraw.LICENSE.txt` ships next to it with the license text of every package the bundle takes code from:

| Package | Version | License |
|---|---|---|
| @babel/runtime | 7.29.10 | MIT |
| @braintree/sanitize-url | 6.0.2 | MIT |
| @excalidraw/excalidraw | 0.18.1 | MIT |
| @excalidraw/laser-pointer | 1.3.1 | MIT |
| @floating-ui/core | 1.8.0 | MIT |
| @floating-ui/dom | 1.8.0 | MIT |
| @floating-ui/react-dom | 2.1.9 | MIT |
| @floating-ui/utils | 0.2.12 | MIT |
| @radix-ui/primitive | 1.0.0 | MIT |
| @radix-ui/primitive | 1.1.1 | MIT |
| @radix-ui/react-arrow | 1.1.2 | MIT |
| @radix-ui/react-collection | 1.0.1 | MIT |
| @radix-ui/react-compose-refs | 1.0.0 | MIT |
| @radix-ui/react-compose-refs | 1.1.1 | MIT |
| @radix-ui/react-context | 1.0.0 | MIT |
| @radix-ui/react-context | 1.1.1 | MIT |
| @radix-ui/react-direction | 1.0.0 | MIT |
| @radix-ui/react-dismissable-layer | 1.1.5 | MIT |
| @radix-ui/react-focus-guards | 1.1.1 | MIT |
| @radix-ui/react-focus-scope | 1.1.2 | MIT |
| @radix-ui/react-id | 1.0.0 | MIT |
| @radix-ui/react-id | 1.1.0 | MIT |
| @radix-ui/react-popover | 1.1.6 | MIT |
| @radix-ui/react-popper | 1.2.2 | MIT |
| @radix-ui/react-portal | 1.1.4 | MIT |
| @radix-ui/react-presence | 1.0.0 | MIT |
| @radix-ui/react-presence | 1.1.2 | MIT |
| @radix-ui/react-primitive | 1.0.1 | MIT |
| @radix-ui/react-primitive | 2.0.2 | MIT |
| @radix-ui/react-roving-focus | 1.0.2 | MIT |
| @radix-ui/react-slot | 1.0.1 | MIT |
| @radix-ui/react-slot | 1.1.2 | MIT |
| @radix-ui/react-tabs | 1.0.2 | MIT |
| @radix-ui/react-use-callback-ref | 1.0.0 | MIT |
| @radix-ui/react-use-callback-ref | 1.1.0 | MIT |
| @radix-ui/react-use-controllable-state | 1.0.0 | MIT |
| @radix-ui/react-use-controllable-state | 1.1.0 | MIT |
| @radix-ui/react-use-escape-keydown | 1.1.0 | MIT |
| @radix-ui/react-use-layout-effect | 1.0.0 | MIT |
| @radix-ui/react-use-layout-effect | 1.1.0 | MIT |
| @radix-ui/react-use-size | 1.1.0 | MIT |
| aria-hidden | 1.2.6 | MIT |
| browser-fs-access | 0.29.1 | Apache-2.0 |
| canvas-roundrect-polyfill | 0.0.1 | MIT |
| clsx | 1.1.1 | MIT |
| crc-32 | 0.3.0 | Apache-2.0 |
| detect-node-es | 1.1.0 | MIT |
| es6-promise-pool | 2.5.0 | MIT |
| fractional-indexing | 3.2.0 | CC0-1.0 |
| fuzzy | 0.1.3 | MIT |
| get-nonce | 1.0.1 | MIT |
| hachure-fill | 0.5.2 | MIT |
| image-blob-reduce | 3.0.1 | MIT |
| jotai | 2.11.0 | MIT |
| jotai-scope | 0.7.2 | MIT |
| lodash.debounce | 4.0.8 | MIT |
| lodash.throttle | 4.1.1 | MIT |
| nanoid | 3.3.3 | MIT |
| open-color | 1.9.1 | MIT |
| pako | 2.0.3 | (MIT AND Zlib) |
| path-data-parser | 0.1.0 | MIT |
| perfect-freehand | 1.2.0 | MIT |
| pica | 7.1.1 | MIT |
| png-chunk-text | 1.0.0 | MIT |
| png-chunks-encode | 1.0.0 | MIT |
| png-chunks-extract | 1.0.0 | MIT |
| points-on-curve | 0.2.0 | MIT |
| points-on-curve | 1.0.1 | MIT |
| points-on-path | 0.2.1 | MIT |
| react | 18.3.1 | MIT |
| react-dom | 18.3.1 | MIT |
| react-remove-scroll | 2.7.2 | MIT |
| react-remove-scroll-bar | 2.3.8 | MIT |
| react-style-singleton | 2.2.3 | MIT |
| roughjs | 4.6.4 | MIT |
| scheduler | 0.23.2 | MIT |
| sliced | 1.0.1 | MIT |
| tslib | 2.8.1 | 0BSD |
| tunnel-rat | 0.1.2 | MIT |
| use-callback-ref | 1.3.3 | MIT |
| use-sidecar | 1.1.3 | MIT |
| use-sync-external-store | 1.7.0 | MIT |
| zustand | 4.5.7 | MIT |

The BPMN viewer and modeler (ADR-0129) are two more bundles, `internal/web/static/vendor/bpmn-viewer.js` and `bpmn-modeler.js`, built from the same directory, with bpmn-js's stylesheets and icon font in `bpmn/`; `bpmn.LICENSE.txt` ships next to them with every license text and notice. bpmn-js is under the bpmn.io license (Copyright (c) 2014-present Camunda Services GmbH): the MIT terms, with the condition that the code displaying the bpmn.io watermark in rendered diagrams is not removed or changed and that the watermark stays fully visible and is not overlapped; the UI keeps it so. htm is under the Apache License 2.0; path-intersection carries a notice for the code it derives from Snap.svg (Copyright 2013 Adobe Systems Incorporated, Apache License 2.0):

| Package | Version | License |
|---|---|---|
| @bpmn-io/diagram-js-ui | 0.2.4 | MIT |
| bpmn-js | 18.31.0 | SEE LICENSE IN LICENSE |
| bpmn-moddle | 10.3.1 | MIT |
| clsx | 2.1.1 | MIT |
| diagram-js | 15.28.1 | MIT |
| diagram-js-direct-editing | 3.6.0 | MIT |
| didi | 11.0.1 | MIT |
| domify | 3.0.0 | MIT |
| htm | 3.1.1 | Apache-2.0 |
| ids | 3.0.2 | MIT |
| inherits-browser | 0.1.0 | ISC |
| min-dash | 5.1.0 | MIT |
| min-dom | 5.3.0 | MIT |
| moddle | 8.2.1 | MIT |
| moddle-xml | 12.3.1 | MIT |
| object-refs | 0.4.0 | MIT |
| path-intersection | 4.2.1 | MIT |
| preact | 10.29.8 | MIT |
| saxen | 11.2.0 | MIT |
| tiny-svg | 4.1.4 | MIT |

The palettes of the drawing editor (ADR-0117), in `internal/web/static/assets/palettes/`, are libraries of the Excalidraw library collection (github.com/excalidraw/excalidraw-libraries, commit 297a349e), which publishes every library under the MIT license (Copyright (c) 2020 Excalidraw): "Excalidraw Archimate Template" by Timothy Galvin, "bpmn" by Aoustin Frederic, "Computers" by EI d'AU, "Network topology icons" by dwelle, "Common Home Network Basics" by DDay987, "Network locations" by Jorge Godoy and "IT icons" by Mateusz Baran. `palettes.LICENSE.txt` ships next to them with the license text and which palette holds which library; `internal/web/vendor/palettes.mjs` builds them. The ArchiMate palette also holds relationships drawn for this project (ADR-0125). The Microsoft palette, `microsoft.json`, is made of the Fluent UI System Icons (github.com/microsoft/fluentui-system-icons, commit 82ecf7cb), which Microsoft publishes under the MIT license (Copyright (c) 2020 Microsoft Corporation), turned into shapes by `internal/web/vendor/fluent.mjs`; it holds no product logos.

The fonts Excalifont, Virgil, Cascadia, Assistant, Liberation Sans, Lilita One and Nunito, shipped with Excalidraw in `internal/web/static/vendor/fonts/`, are licensed under the SIL Open Font License 1.1; Comic Shanns is licensed under the MIT license.

The comic theme (ADR-0085) uses the font Comic Neue, Google Fonts release v9 (Copyright 2014 The Comic Neue Project Authors), from the package @fontsource/comic-neue 5.3.0, licensed under the SIL Open Font License 1.1; `internal/web/static/assets/fonts/comic-neue.LICENSE.txt` ships next to it. The sketch theme uses Virgil from the Excalidraw fonts above.

The Swiss theme (ADR-0120) follows the design system of the Swiss Confederation, github.com/swiss/designsystem (commit d26f6988, MIT License, Copyright (c) Schweizerische Eidgenossenschaft): its colour values, radii, shadows and the look of its buttons, fields, tables and bars are taken into `internal/site/themes/swiss.css`; no code of it is shipped. The theme uses the font Noto Sans (Copyright 2022 The Noto Project Authors), its Latin subset from the package @fontsource/noto-sans 5.3.0, licensed under the SIL Open Font License 1.1; `internal/web/static/assets/fonts/noto-sans.LICENSE.txt` ships next to it.

The web UI's stylesheet, `internal/web/static/assets/style.css`, adapts the palette, type, spacing and component look of the console of [pblumer/atlas](https://github.com/pblumer/atlas) (`api/web/app.css`), licensed under AGPL-3.0-only like this project. No atlas name, logo or font is included.

The wordmark of the logo (`docs/brand`, and its copies in `internal/web/static`) is drawn from the outlines of [Inter](https://rsms.me/inter/) Display by Rasmus Andersson, licensed under the SIL Open Font License 1.1. The outlines are part of the artwork; no font file is included.

The Go standard library and runtime are under the BSD-3-Clause license of the Go project.

## NOTICE of github.com/coreos/go-oidc/v3

```text
CoreOS Project
Copyright 2014 CoreOS, Inc

This product includes software developed at CoreOS, Inc.
(http://www.coreos.com/).
```

## NOTICE of github.com/prometheus/client_golang

```text
Prometheus instrumentation library for Go applications
Copyright 2012-2015 The Prometheus Authors

This product includes software developed at
SoundCloud Ltd. (http://soundcloud.com/).

The following components are included in this product:

perks - a fork of https://github.com/bmizerany/perks
https://github.com/beorn7/perks
Copyright 2013-2015 Blake Mizerany, Björn Rabenstein
See https://github.com/beorn7/perks/blob/master/README.md for license details.

Go support for Protocol Buffers - Google's data interchange format
http://github.com/golang/protobuf/
Copyright 2010 The Go Authors
See source code for license details.
```

## NOTICE of github.com/prometheus/client_model

```text
Data model artifacts for Prometheus.
Copyright 2012-2015 The Prometheus Authors

This product includes software developed at
SoundCloud Ltd. (http://soundcloud.com/).
```

## NOTICE of github.com/prometheus/common

```text
Common libraries shared by Prometheus Go components.
Copyright 2015 The Prometheus Authors

This product includes software developed at
SoundCloud Ltd. (http://soundcloud.com/).
```

## NOTICE of github.com/prometheus/procfs

```text
procfs provides functions to retrieve system, kernel and process
metrics from the pseudo-filesystem proc.

Copyright 2014-2015 The Prometheus Authors

This product includes software developed at
SoundCloud Ltd. (http://soundcloud.com/).
```

## Apache License 2.0 (github.com/coreos/go-oidc/v3, github.com/go-jose/go-jose/v4, github.com/prometheus/client_golang, github.com/prometheus/client_model, github.com/prometheus/common, github.com/prometheus/procfs)

```text
Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

   TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

   1. Definitions.

      "License" shall mean the terms and conditions for use, reproduction,
      and distribution as defined by Sections 1 through 9 of this document.

      "Licensor" shall mean the copyright owner or entity authorized by
      the copyright owner that is granting the License.

      "Legal Entity" shall mean the union of the acting entity and all
      other entities that control, are controlled by, or are under common
      control with that entity. For the purposes of this definition,
      "control" means (i) the power, direct or indirect, to cause the
      direction or management of such entity, whether by contract or
      otherwise, or (ii) ownership of fifty percent (50%) or more of the
      outstanding shares, or (iii) beneficial ownership of such entity.

      "You" (or "Your") shall mean an individual or Legal Entity
      exercising permissions granted by this License.

      "Source" form shall mean the preferred form for making modifications,
      including but not limited to software source code, documentation
      source, and configuration files.

      "Object" form shall mean any form resulting from mechanical
      transformation or translation of a Source form, including but
      not limited to compiled object code, generated documentation,
      and conversions to other media types.

      "Work" shall mean the work of authorship, whether in Source or
      Object form, made available under the License, as indicated by a
      copyright notice that is included in or attached to the work
      (an example is provided in the Appendix below).

      "Derivative Works" shall mean any work, whether in Source or Object
      form, that is based on (or derived from) the Work and for which the
      editorial revisions, annotations, elaborations, or other modifications
      represent, as a whole, an original work of authorship. For the purposes
      of this License, Derivative Works shall not include works that remain
      separable from, or merely link (or bind by name) to the interfaces of,
      the Work and Derivative Works thereof.

      "Contribution" shall mean any work of authorship, including
      the original version of the Work and any modifications or additions
      to that Work or Derivative Works thereof, that is intentionally
      submitted to Licensor for inclusion in the Work by the copyright owner
      or by an individual or Legal Entity authorized to submit on behalf of
      the copyright owner. For the purposes of this definition, "submitted"
      means any form of electronic, verbal, or written communication sent
      to the Licensor or its representatives, including but not limited to
      communication on electronic mailing lists, source code control systems,
      and issue tracking systems that are managed by, or on behalf of, the
      Licensor for the purpose of discussing and improving the Work, but
      excluding communication that is conspicuously marked or otherwise
      designated in writing by the copyright owner as "Not a Contribution."

      "Contributor" shall mean Licensor and any individual or Legal Entity
      on behalf of whom a Contribution has been received by Licensor and
      subsequently incorporated within the Work.

   2. Grant of Copyright License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      copyright license to reproduce, prepare Derivative Works of,
      publicly display, publicly perform, sublicense, and distribute the
      Work and such Derivative Works in Source or Object form.

   3. Grant of Patent License. Subject to the terms and conditions of
      this License, each Contributor hereby grants to You a perpetual,
      worldwide, non-exclusive, no-charge, royalty-free, irrevocable
      (except as stated in this section) patent license to make, have made,
      use, offer to sell, sell, import, and otherwise transfer the Work,
      where such license applies only to those patent claims licensable
      by such Contributor that are necessarily infringed by their
      Contribution(s) alone or by combination of their Contribution(s)
      with the Work to which such Contribution(s) was submitted. If You
      institute patent litigation against any entity (including a
      cross-claim or counterclaim in a lawsuit) alleging that the Work
      or a Contribution incorporated within the Work constitutes direct
      or contributory patent infringement, then any patent licenses
      granted to You under this License for that Work shall terminate
      as of the date such litigation is filed.

   4. Redistribution. You may reproduce and distribute copies of the
      Work or Derivative Works thereof in any medium, with or without
      modifications, and in Source or Object form, provided that You
      meet the following conditions:

      (a) You must give any other recipients of the Work or
          Derivative Works a copy of this License; and

      (b) You must cause any modified files to carry prominent notices
          stating that You changed the files; and

      (c) You must retain, in the Source form of any Derivative Works
          that You distribute, all copyright, patent, trademark, and
          attribution notices from the Source form of the Work,
          excluding those notices that do not pertain to any part of
          the Derivative Works; and

      (d) If the Work includes a "NOTICE" text file as part of its
          distribution, then any Derivative Works that You distribute must
          include a readable copy of the attribution notices contained
          within such NOTICE file, excluding those notices that do not
          pertain to any part of the Derivative Works, in at least one
          of the following places: within a NOTICE text file distributed
          as part of the Derivative Works; within the Source form or
          documentation, if provided along with the Derivative Works; or,
          within a display generated by the Derivative Works, if and
          wherever such third-party notices normally appear. The contents
          of the NOTICE file are for informational purposes only and
          do not modify the License. You may add Your own attribution
          notices within Derivative Works that You distribute, alongside
          or as an addendum to the NOTICE text from the Work, provided
          that such additional attribution notices cannot be construed
          as modifying the License.

      You may add Your own copyright statement to Your modifications and
      may provide additional or different license terms and conditions
      for use, reproduction, or distribution of Your modifications, or
      for any such Derivative Works as a whole, provided Your use,
      reproduction, and distribution of the Work otherwise complies with
      the conditions stated in this License.

   5. Submission of Contributions. Unless You explicitly state otherwise,
      any Contribution intentionally submitted for inclusion in the Work
      by You to the Licensor shall be under the terms and conditions of
      this License, without any additional terms or conditions.
      Notwithstanding the above, nothing herein shall supersede or modify
      the terms of any separate license agreement you may have executed
      with Licensor regarding such Contributions.

   6. Trademarks. This License does not grant permission to use the trade
      names, trademarks, service marks, or product names of the Licensor,
      except as required for reasonable and customary use in describing the
      origin of the Work and reproducing the content of the NOTICE file.

   7. Disclaimer of Warranty. Unless required by applicable law or
      agreed to in writing, Licensor provides the Work (and each
      Contributor provides its Contributions) on an "AS IS" BASIS,
      WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
      implied, including, without limitation, any warranties or conditions
      of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
      PARTICULAR PURPOSE. You are solely responsible for determining the
      appropriateness of using or redistributing the Work and assume any
      risks associated with Your exercise of permissions under this License.

   8. Limitation of Liability. In no event and under no legal theory,
      whether in tort (including negligence), contract, or otherwise,
      unless required by applicable law (such as deliberate and grossly
      negligent acts) or agreed to in writing, shall any Contributor be
      liable to You for damages, including any direct, indirect, special,
      incidental, or consequential damages of any character arising as a
      result of this License or out of the use or inability to use the
      Work (including but not limited to damages for loss of goodwill,
      work stoppage, computer failure or malfunction, or any and all
      other commercial damages or losses), even if such Contributor
      has been advised of the possibility of such damages.

   9. Accepting Warranty or Additional Liability. While redistributing
      the Work or Derivative Works thereof, You may choose to offer,
      and charge a fee for, acceptance of support, warranty, indemnity,
      or other liability obligations and/or rights consistent with this
      License. However, in accepting such obligations, You may act only
      on Your own behalf and on Your sole responsibility, not on behalf
      of any other Contributor, and only if You agree to indemnify,
      defend, and hold each Contributor harmless for any liability
      incurred by, or claims asserted against, such Contributor by reason
      of your accepting any such warranty or additional liability.

   END OF TERMS AND CONDITIONS

   APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "{}"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

   Copyright {yyyy} {name of copyright owner}

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
```

## BSD-3-Clause (golang.org/x/oauth2)

```text
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
```

## BSD-3-Clause (golang.org/x/sys)

```text
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
```

## BSD-3-Clause (google.golang.org/protobuf)

```text
Copyright (c) 2018 The Go Authors. All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

   * Redistributions of source code must retain the above copyright
notice, this list of conditions and the following disclaimer.
   * Redistributions in binary form must reproduce the above
copyright notice, this list of conditions and the following disclaimer
in the documentation and/or other materials provided with the
distribution.
   * Neither the name of Google Inc. nor the names of its
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
```

## BSD-3-Clause (github.com/munnerz/goautoneg)

```text
Copyright (c) 2011, Open Knowledge Foundation Ltd.
All rights reserved.

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are
met:

    Redistributions of source code must retain the above copyright
    notice, this list of conditions and the following disclaimer.

    Redistributions in binary form must reproduce the above copyright
    notice, this list of conditions and the following disclaimer in
    the documentation and/or other materials provided with the
    distribution.

    Neither the name of the Open Knowledge Foundation Ltd. nor the
    names of its contributors may be used to endorse or promote
    products derived from this software without specific prior written
    permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
"AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

## MIT (github.com/beorn7/perks)

```text
Copyright (C) 2013 Blake Mizerany

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```

## MIT (github.com/cespare/xxhash/v2)

```text
Copyright (c) 2016 Caleb Spare

MIT License

Permission is hereby granted, free of charge, to any person obtaining
a copy of this software and associated documentation files (the
"Software"), to deal in the Software without restriction, including
without limitation the rights to use, copy, modify, merge, publish,
distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to
the following conditions:

The above copyright notice and this permission notice shall be
included in all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
```
