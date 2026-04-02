

#  Usage:
#  make            Build telefonist (default)
#  make alsa       Build telefonist with ALSA
#  make static     Build fully static telefonist with Zig
#  make compress   Compress telefonist binary with UPX
#  make test       Run tests
#  make test_alsa  Run tests with ALSA
#  make clean      Remove build artifacts (keeps source/tarballs)
#  make distclean  Remove EVERYTHING under libbaresip/


define check_alsa
	@if [ "$(1)" = "noalsa" ] && [ -f "$(ALSA_MARKER)" ]; then \
		echo "ERROR: telefonist was compiled WITH ALSA, but you are building telefonist WITHOUT it."; \
		echo "       Run 'make distclean && make alsa' instead to match the library build."; \
		echo "       If you don't want ALSA, run 'make clean' and 'make' in the telefonist library directory first."; \
		exit 1; \
	elif [ "$(1)" = "alsa" ] && [ ! -f "$(ALSA_MARKER)" ]; then \
		echo "ERROR: telefonist was compiled WITHOUT ALSA (or marker missing), but you are building telefonist WITH it."; \
		echo "       Run 'make distclean && make' instead to match the library build."; \
		echo "       If you need ALSA, run 'make clean' and 'make alsa' in the telefonist library directory first."; \
		exit 1; \
	fi
endef

SHELL := /bin/sh

VERSION ?= 0.8.3
JOBS ?= 16
CC ?= gcc
CXX ?= g++

OPUS_VER ?= 1.6.1
OPENSSL_VER ?= 3.0.13
G722_VER ?= master
SNDFILE_VER ?= 1.2.2

# Paths (all relative to this Makefile dir)
ROOT_DIR := $(CURDIR)
LIBBARESIP_DIR := $(ROOT_DIR)/libbaresip
GIT_DIR := $(LIBBARESIP_DIR)/git

OPENSSL_PREFIX := $(GIT_DIR)/openssl-prefix
OPENSSL_TAR := $(GIT_DIR)/openssl-$(OPENSSL_VER).tar.gz
OPENSSL_SRC := $(GIT_DIR)/openssl-$(OPENSSL_VER)

OPUS_TAR := $(GIT_DIR)/opus-$(OPUS_VER).tar.gz
OPUS_SRC := $(GIT_DIR)/opus-$(OPUS_VER)

RE_DIR := $(GIT_DIR)/re
REM_DIR := $(GIT_DIR)/rem
BARESIP_DIR := $(GIT_DIR)/baresip
G722_SRC := $(GIT_DIR)/libg722
SNDFILE_TAR := $(GIT_DIR)/libsndfile-$(SNDFILE_VER).tar.xz
SNDFILE_SRC := $(GIT_DIR)/libsndfile-$(SNDFILE_VER)

# Staging dirs consumed by CGO
STAGE_OPENSSL := $(LIBBARESIP_DIR)/openssl
STAGE_OPUS := $(LIBBARESIP_DIR)/opus
STAGE_RE := $(LIBBARESIP_DIR)/re
STAGE_REM := $(LIBBARESIP_DIR)/rem
STAGE_BARESIP := $(LIBBARESIP_DIR)/baresip
STAGE_G722 := $(LIBBARESIP_DIR)/g722
STAGE_SNDFILE := $(LIBBARESIP_DIR)/sndfile

ALSA_MARKER := $(LIBBARESIP_DIR)/baresip/libbaresip.a.alsa

# Outputs
OPENSSL_LIBSSL := $(STAGE_OPENSSL)/libssl.a
OPENSSL_LIBCRYPTO := $(STAGE_OPENSSL)/libcrypto.a

OPUS_LIB := $(STAGE_OPUS)/libopus.a
RE_LIB := $(STAGE_RE)/libre.a
REM_LIB := $(STAGE_REM)/librem.a
BARESIP_LIB := $(STAGE_BARESIP)/libbaresip.a
BARESIP_EXE := $(STAGE_BARESIP)/baresip
G722_LIB := $(STAGE_G722)/libg722.a
SNDFILE_LIB := $(STAGE_SNDFILE)/libsndfile.a

# Header staging markers
HDR_STAGE_RE := $(STAGE_RE)/include/re.h
HDR_STAGE_REM := $(STAGE_REM)/include/rem.h
HDR_STAGE_BARESIP := $(STAGE_BARESIP)/include/baresip.h

# Common module sets
BASE_MODULES := account contact cons ctrl_tcp debug_cmd echo httpd menu mwi netroam natpmp presence ice stun turn audioreport serreg uuid stdio
AUDIO_MODULES_NOALSA := aubridge aufile ausine auconv auresamp mixminus sndfile
AUDIO_MODULES_ALSA := alsa $(AUDIO_MODULES_NOALSA)

# Use *your* custom g722 module (baresip module name: "g722") instead of built-in "libg722"
CODEC_MODULES := g711 g722 opus

TLS_MODULES := dtls_srtp srtp

# Directory where we stage all built *.so modules for runtime dlopen()
STAGE_MODULES_DIR := $(STAGE_BARESIP)/modules

# Convert to CMake list (semicolon-separated)
MODULES_NOALSA := $(BASE_MODULES) $(AUDIO_MODULES_NOALSA) $(CODEC_MODULES) $(TLS_MODULES)
MODULES_ALSA := $(BASE_MODULES) $(AUDIO_MODULES_ALSA) $(CODEC_MODULES) $(TLS_MODULES)

# NOTE: These are passed to baresip CMake build (EXTRA_CFLAGS / EXTRA_LFLAGS).
SL_EXTRA_CFLAGS := -I ../opus/include -I ../g722
SL_EXTRA_LFLAGS := -L ../opus -L ../openssl -L ../g722 ../openssl/libssl.a ../openssl/libcrypto.a -lg722

# OpenSSL pkgconfig path for CMake isolation
OPENSSL_LIBDIR_PC := $(shell if [ -d "$(OPENSSL_PREFIX)/lib64/pkgconfig" ]; then echo "lib64"; else echo "lib"; fi)
OPENSSL_PKG_CONFIG_PATH := $(OPENSSL_PREFIX)/$(OPENSSL_LIBDIR_PC)/pkgconfig

# libre (re) zlib control:
# - re defines USE_ZLIB if CMake finds zlib (find_package(ZLIB)).
# - There is no direct "USE_ZLIB" CMake option in re; to force-disable it,
#   we disable package lookup.
RE_ZLIB_CMAKE_FLAGS := -DCMAKE_DISABLE_FIND_PACKAGE_ZLIB=TRUE

.PHONY: all alsa static baresip clean distclean help stage_headers stage_patches telefonist telefonist_alsa telefonist_static test test_alsa compress

all: telefonist
	@echo "Done. Artifacts staged under: libbaresip/{re,rem,baresip,openssl,opus,g722,sndfile}"

alsa: telefonist_alsa
	@echo "Done (ALSA+zlib enabled). Artifacts staged under: libbaresip/{re,rem,baresip,openssl,opus,g722,sndfile}"

telefonist: $(BARESIP_LIB) stage_headers
	$(call check_alsa,noalsa)
	@echo "Building telefonist binary..."
	CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X github.com/negbie/telefonist/pkg/telefonist.Version=$(VERSION)" -o telefonist ./cmd/telefonist

telefonist_alsa: MODULES := $(MODULES_ALSA)
telefonist_alsa: RE_ZLIB_CMAKE_FLAGS :=
telefonist_alsa: $(BARESIP_LIB).alsa stage_headers
	$(call check_alsa,alsa)
	@echo "Building telefonist binary with ALSA..."
	CGO_ENABLED=1 go build -trimpath -tags alsa -ldflags="-s -w -X github.com/negbie/telefonist/pkg/telefonist.Version=$(VERSION)" -o telefonist ./cmd/telefonist

static: telefonist_static

telefonist_static: export CC := zig cc -target x86_64-linux-musl
telefonist_static: export CXX := zig c++ -target x86_64-linux-musl
telefonist_static: $(BARESIP_LIB) stage_headers
	$(call check_alsa,noalsa)
	@echo "Building fully static telefonist binary with Zig..."
	CGO_ENABLED=1 \
	go build -trimpath -o telefonist \
		-ldflags "-s -w -X github.com/negbie/telefonist/pkg/telefonist.Version=$(VERSION) -linkmode external -extldflags '-static'" \
		./cmd/telefonist

compress: telefonist_static
	@echo "Compressing telefonist binary with UPX..."
	upx --best --lzma telefonist

help:
	@printf "%s\n" \
	  "Targets:" \
	  "  make            Build telefonist (default)" \
	  "  make alsa       Build telefonist with ALSA" \
	  "  make static     Build fully static telefonist with Zig" \
	  "  make compress   Compress telefonist binary with UPX" \
	  "  make test       Run tests" \
	  "  make test_alsa  Run tests with ALSA" \
	  "  make clean      Remove build artifacts (keeps source/tarballs)" \
	  "  make distclean  Remove EVERYTHING under libbaresip/" \
	  "" \
	  "Variables:" \
	  "  JOBS=$(JOBS) OPUS_VER=$(OPUS_VER) OPENSSL_VER=$(OPENSSL_VER)"

# Ensure base directory layout exists
$(LIBBARESIP_DIR):
	@mkdir -p "$(LIBBARESIP_DIR)"

$(GIT_DIR): | $(LIBBARESIP_DIR)
	@mkdir -p "$(GIT_DIR)" "$(STAGE_RE)" "$(STAGE_REM)" "$(STAGE_BARESIP)" "$(STAGE_OPUS)" "$(STAGE_OPENSSL)" "$(STAGE_G722)" "$(STAGE_SNDFILE)"

###############################################################################
# Custom g722 module staging (copy into vendored baresip tree like baresip-apps)
###############################################################################
.PHONY: stage_g722
stage_g722: $(BARESIP_DIR)
	@set -eu; \
	if [ ! -d "$(ROOT_DIR)/pkg/gobaresip/g722" ]; then \
	  echo "ERROR: custom g722 module directory not found at: $(ROOT_DIR)/pkg/gobaresip/g722" >&2; \
	  exit 1; \
	fi; \
	src_dir="$(ROOT_DIR)/pkg/gobaresip/g722"; \
	mkdir -p "$(BARESIP_DIR)/modules"; \
	rm -rf "$(BARESIP_DIR)/modules/g722"; \
	cp -a "$$src_dir" "$(BARESIP_DIR)/modules/"; \
	sed -i 's|@STAGE_G722@|$(STAGE_G722)|g' "$(BARESIP_DIR)/modules/g722/CMakeLists.txt"

###############################################################################
# Build-time source patching
###############################################################################
stage_patches: $(BARESIP_DIR)
	@if [ -f "patches.txt" ]; then \
	  echo "stage_patches: applying patches from patches.txt"; \
	  go run _tools/patcher/patcher.go patches.txt; \
	fi

###############################################################################
# Custom audioreport module staging (copy into vendored baresip tree)
###############################################################################
.PHONY: stage_audioreport
stage_audioreport: $(BARESIP_DIR)
	@set -eu; \
	if [ ! -d "$(ROOT_DIR)/pkg/gobaresip/audioreport" ]; then \
	  echo "ERROR: custom audioreport module directory not found at: $(ROOT_DIR)/pkg/gobaresip/audioreport" >&2; \
	  exit 1; \
	fi; \
	src_dir="$(ROOT_DIR)/pkg/gobaresip/audioreport"; \
	mkdir -p "$(BARESIP_DIR)/modules"; \
	rm -rf "$(BARESIP_DIR)/modules/audioreport"; \
	cp -a "$$src_dir" "$(BARESIP_DIR)/modules/"

###############################################################################
# Custom presence module staging (copy into vendored baresip tree)
###############################################################################
.PHONY: stage_presence
stage_presence: $(BARESIP_DIR)
	@set -eu; \
	if [ ! -d "$(ROOT_DIR)/pkg/gobaresip/presence" ]; then \
	  echo "ERROR: custom presence module directory not found at: $(ROOT_DIR)/pkg/gobaresip/presence" >&2; \
	  exit 1; \
	fi; \
	src_dir="$(ROOT_DIR)/pkg/gobaresip/presence"; \
	mkdir -p "$(BARESIP_DIR)/modules"; \
	rm -rf "$(BARESIP_DIR)/modules/presence"; \
	cp -a "$$src_dir" "$(BARESIP_DIR)/modules/"

###############################################################################
# Custom aufile module staging (copy into vendored baresip tree)
###############################################################################
.PHONY: stage_aufile
stage_aufile: $(BARESIP_DIR)
	@set -eu; \
	if [ ! -d "$(ROOT_DIR)/pkg/gobaresip/aufile" ]; then \
	  echo "ERROR: custom aufile module directory not found at: $(ROOT_DIR)/pkg/gobaresip/aufile" >&2; \
	  exit 1; \
	fi; \
	src_dir="$(ROOT_DIR)/pkg/gobaresip/aufile"; \
	mkdir -p "$(BARESIP_DIR)/modules"; \
	rm -rf "$(BARESIP_DIR)/modules/aufile"; \
	cp -a "$$src_dir" "$(BARESIP_DIR)/modules/"


###############################################################################
# Module .so staging (so runtime dlopen can find them)
###############################################################################
.PHONY: stage_modules
stage_modules: $(BARESIP_DIR)
	@set -eu; \
	mkdir -p "$(STAGE_MODULES_DIR)"; \
	if [ -d "$(BARESIP_DIR)/build/modules" ]; then \
	  find "$(BARESIP_DIR)/build/modules" -maxdepth 2 -type f -name '*.so' -exec cp -a '{}' "$(STAGE_MODULES_DIR)/" ';' || true; \
	fi

###############################################################################
# OpenSSL (static)
###############################################################################
$(OPENSSL_TAR): | $(GIT_DIR)
	@cd "$(GIT_DIR)" && wget "https://www.openssl.org/source/openssl-$(OPENSSL_VER).tar.gz"

$(OPENSSL_SRC): $(OPENSSL_TAR)
	@cd "$(GIT_DIR)" && tar -xzf "openssl-$(OPENSSL_VER).tar.gz"

# Build/install OpenSSL into OPENSSL_PREFIX
# Then stage static libs under libbaresip/openssl/ for CGO.
$(OPENSSL_LIBSSL) $(OPENSSL_LIBCRYPTO): $(OPENSSL_SRC) | $(GIT_DIR)
	@set -eu; \
	cd "$(OPENSSL_SRC)"; \
	CC="$(CC)" ./Configure linux-x86_64 no-shared no-zlib no-comp no-tests no-unit-test no-external-tests no-engine no-legacy --prefix="$(OPENSSL_PREFIX)" --openssldir="$(OPENSSL_PREFIX)"; \
	make -j"$(JOBS)"; \
	make install_sw; \
	openssl_libdir=""; \
	if [ -d "$(OPENSSL_PREFIX)/lib" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib"; \
	elif [ -d "$(OPENSSL_PREFIX)/lib64" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib64"; \
	else echo "ERROR: OpenSSL install did not create lib or lib64 under $(OPENSSL_PREFIX)" >&2; exit 1; fi; \
	mkdir -p "$(STAGE_OPENSSL)"; \
	cp "$$openssl_libdir/libssl.a" "$(OPENSSL_LIBSSL)"; \
	cp "$$openssl_libdir/libcrypto.a" "$(OPENSSL_LIBCRYPTO)"

###############################################################################
# re (libre.a) and rem (librem.a) - CMake builds against OpenSSL prefix
###############################################################################
$(RE_DIR): | $(GIT_DIR)
	@if [ ! -d "$(RE_DIR)" ]; then cd "$(GIT_DIR)" && git clone https://github.com/baresip/re.git; fi

$(REM_DIR): | $(GIT_DIR)
	@if [ ! -d "$(REM_DIR)" ]; then cd "$(GIT_DIR)" && git clone https://github.com/baresip/rem.git; fi

$(RE_LIB): $(RE_DIR) $(OPENSSL_LIBSSL) $(OPENSSL_LIBCRYPTO) | $(GIT_DIR)
	@set -eu; \
	cd "$(RE_DIR)"; \
	mkdir -p build; \
	openssl_libdir=""; \
	if [ -d "$(OPENSSL_PREFIX)/lib" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib"; \
	elif [ -d "$(OPENSSL_PREFIX)/lib64" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib64"; fi; \
	CC="$(CC)" CXX="$(CXX)" PKG_CONFIG_PATH="$(OPENSSL_PKG_CONFIG_PATH)" cmake -S . -B build \
	  -DCMAKE_BUILD_TYPE=Release \
	  -DOPENSSL_ROOT_DIR="$(OPENSSL_PREFIX)" \
	  -DOPENSSL_INCLUDE_DIR="$(OPENSSL_PREFIX)/include" \
	  -DOPENSSL_SSL_LIBRARY="$$openssl_libdir/libssl.a" \
	  -DOPENSSL_CRYPTO_LIBRARY="$$openssl_libdir/libcrypto.a" \
	  $(RE_ZLIB_CMAKE_FLAGS) \
	  -DCMAKE_FIND_ROOT_PATH="$(ROOT_DIR)" \
	  -DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=BOTH \
	  -DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=BOTH; \
	cmake --build build --target re -- -j"$(JOBS)"; \
	mkdir -p "$(STAGE_RE)"; \
	cp build/libre.a "$(RE_LIB)"

$(REM_LIB): $(REM_DIR) $(OPENSSL_LIBSSL) $(OPENSSL_LIBCRYPTO) | $(GIT_DIR)
	@set -eu; \
	cd "$(REM_DIR)"; \
	mkdir -p build; \
	openssl_libdir=""; \
	if [ -d "$(OPENSSL_PREFIX)/lib" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib"; \
	elif [ -d "$(OPENSSL_PREFIX)/lib64" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib64"; fi; \
	CC="$(CC)" CXX="$(CXX)" PKG_CONFIG_PATH="$(OPENSSL_PKG_CONFIG_PATH)" cmake -S . -B build \
	  -DCMAKE_BUILD_TYPE=Release \
	  -DOPENSSL_ROOT_DIR="$(OPENSSL_PREFIX)" \
	  -DOPENSSL_INCLUDE_DIR="$(OPENSSL_PREFIX)/include" \
	  -DOPENSSL_SSL_LIBRARY="$$openssl_libdir/libssl.a" \
	  -DOPENSSL_CRYPTO_LIBRARY="$$openssl_libdir/libcrypto.a" \
	  $(RE_ZLIB_CMAKE_FLAGS) \
	  -DCMAKE_FIND_ROOT_PATH="$(ROOT_DIR)" \
	  -DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=BOTH \
	  -DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=BOTH; \
	cmake --build build --target rem -- -j"$(JOBS)"; \
	mkdir -p "$(STAGE_REM)"; \
	cp build/librem.a "$(REM_LIB)"

# Ensure our custom modules are present in the baresip/modules tree before building baresip
$(BARESIP_LIB): stage_g722 stage_audioreport stage_presence stage_aufile
$(BARESIP_LIB).alsa: stage_g722 stage_audioreport stage_presence stage_aufile
$(BARESIP_EXE): stage_g722 stage_audioreport stage_presence stage_aufile

# After (re)building baresip, stage any produced shared-object modules for runtime dlopen()
all: stage_modules
alsa: stage_modules
baresip: stage_modules

###############################################################################
# Opus (static)
###############################################################################
$(OPUS_TAR): | $(GIT_DIR)
	@cd "$(GIT_DIR)" && wget "https://downloads.xiph.org/releases/opus/opus-$(OPUS_VER).tar.gz"

$(OPUS_SRC): $(OPUS_TAR)
	@cd "$(GIT_DIR)" && tar -xzf "opus-$(OPUS_VER).tar.gz"

$(OPUS_LIB): $(OPUS_SRC) | $(GIT_DIR)
	@set -eu; \
	cd "$(OPUS_SRC)"; \
	CC="$(CC)" ./configure --with-pic; \
	make -j"$(JOBS)"; \
	mkdir -p "$(STAGE_OPUS)/include/opus"; \
	cp "$(OPUS_SRC)/.libs/libopus.a" "$(OPUS_LIB)"; \
	cp "$(OPUS_SRC)/include/"*.h "$(STAGE_OPUS)/include/opus/"

###############################################################################
# G.722 from Sippy (static libg722.a)
###############################################################################
$(G722_SRC): | $(GIT_DIR)
	@if [ ! -d "$(G722_SRC)" ]; then cd "$(GIT_DIR)" && git clone https://github.com/sippy/libg722.git; fi

$(G722_LIB): $(G722_SRC) | $(GIT_DIR)
	@set -eu; \
	cd "$(G722_SRC)"; \
	$(CC) -fPIC -O3 -c -o g722_dec.o g722_decode.c; \
	$(CC) -fPIC -O3 -c -o g722_enc.o g722_encode.c; \
	ar rcs libg722.a g722_dec.o g722_enc.o; \
	mkdir -p "$(STAGE_G722)"; \
	cp libg722.a "$(G722_LIB)"; \
	cp g722.h g722_encoder.h g722_decoder.h "$(STAGE_G722)/"

###############################################################################
# libsndfile (static libsndfile.a)
###############################################################################
$(SNDFILE_TAR): | $(GIT_DIR)
	@cd "$(GIT_DIR)" && wget "https://github.com/libsndfile/libsndfile/releases/download/$(SNDFILE_VER)/libsndfile-$(SNDFILE_VER).tar.xz"

$(SNDFILE_SRC): $(SNDFILE_TAR)
	@cd "$(GIT_DIR)" && tar -xJf "libsndfile-$(SNDFILE_VER).tar.xz"

$(SNDFILE_LIB): $(SNDFILE_SRC) | $(GIT_DIR)
	@set -eu; \
	cd "$(SNDFILE_SRC)"; \
	CC="$(CC)" CFLAGS="-O2 -fPIC -std=gnu11" ./configure --with-pic --disable-shared --enable-static --disable-external-libs --disable-mpeg; \
	make -j"$(JOBS)"; \
	mkdir -p "$(STAGE_SNDFILE)/include"; \
	cp "src/.libs/libsndfile.a" "$(SNDFILE_LIB)"; \
	cp "include/sndfile.h" "$(STAGE_SNDFILE)/include/"

###############################################################################
# baresip (static libbaresip.a) - CMake out-of-source build
###############################################################################
$(BARESIP_DIR): | $(GIT_DIR)
	@if [ ! -d "$(BARESIP_DIR)" ]; then cd "$(GIT_DIR)" && git clone https://github.com/baresip/baresip.git; fi

# Default (no ALSA) build uses MODULES_NOALSA
MODULES := $(MODULES_NOALSA)

$(BARESIP_LIB): stage_patches $(BARESIP_DIR) $(RE_LIB) $(REM_LIB) $(OPUS_LIB) $(G722_LIB) $(SNDFILE_LIB) $(OPENSSL_LIBSSL) $(OPENSSL_LIBCRYPTO) | $(GIT_DIR)
	@set -eu; \
	cd "$(BARESIP_DIR)"; \
	mkdir -p build; \
	cd build; \
	openssl_libdir=""; \
	if [ -d "$(OPENSSL_PREFIX)/lib" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib"; \
	elif [ -d "$(OPENSSL_PREFIX)/lib64" ]; then openssl_libdir="$(OPENSSL_PREFIX)/lib64"; fi; \
	CC="$(CC)" CXX="$(CXX)" PKG_CONFIG_PATH="$(OPENSSL_PKG_CONFIG_PATH)" cmake .. \
	  -DCMAKE_BUILD_TYPE=Release \
	  -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
	  -DSTATIC=ON \
	  -DMODULES="$$(printf "%s" "$(MODULES)" | tr ' ' ';')" \
	  -DOPENSSL_ROOT_DIR="$(OPENSSL_PREFIX)" \
	  -DOPENSSL_INCLUDE_DIR="$(OPENSSL_PREFIX)/include" \
	  -DOPENSSL_SSL_LIBRARY="$$openssl_libdir/libssl.a" \
	  -DOPENSSL_CRYPTO_LIBRARY="$$openssl_libdir/libcrypto.a" \
	  $(RE_ZLIB_CMAKE_FLAGS) \
	  -DOPUS_INCLUDE_DIR="$(STAGE_OPUS)/include" \
	  -DOPUS_LIBRARY="$(OPUS_LIB)" \
	  -DSNDFILE_INCLUDE_DIR="$(STAGE_SNDFILE)/include" \
	  -DSNDFILE_LIBRARIES="$(SNDFILE_LIB)" \
	  -DCMAKE_C_FLAGS="-I$(STAGE_OPUS)/include -I$(STAGE_G722) -I$(STAGE_SNDFILE)/include" \
	  -DCMAKE_EXE_LINKER_FLAGS="-L$(STAGE_OPUS) -L$(STAGE_OPENSSL) -L$(STAGE_G722) -L$(STAGE_SNDFILE) -lssl -lcrypto -lg722 -lsndfile" \
	  -DCMAKE_FIND_ROOT_PATH="$(ROOT_DIR)" \
	  -DCMAKE_FIND_ROOT_PATH_MODE_INCLUDE=ONLY \
	  -DCMAKE_FIND_ROOT_PATH_MODE_LIBRARY=ONLY; \
	cmake --build . --target baresip -- -j"$(JOBS)"; \
	mkdir -p "$(STAGE_BARESIP)"; \
	if [ -f "libbaresip.a" ]; then cp "libbaresip.a" "$(BARESIP_LIB)"; \
	elif [ -f "src/libbaresip.a" ]; then cp "src/libbaresip.a" "$(BARESIP_LIB)"; \
	elif [ -f "lib/libbaresip.a" ]; then cp "lib/libbaresip.a" "$(BARESIP_LIB)"; \
	else echo "ERROR: libbaresip.a not found after CMake build." >&2; \
	     echo "Searched: build/libbaresip.a, build/src/libbaresip.a, build/lib/libbaresip.a" >&2; \
	     exit 1; fi

# ALSA build marker target: we reuse the same staged output location for libbaresip.a.
# If you want both variants side-by-side, adjust this to copy to a different filename.
$(BARESIP_LIB).alsa: MODULES := $(MODULES_ALSA)
$(BARESIP_LIB).alsa: $(BARESIP_LIB)
	@# Marker file to indicate ALSA variant was requested/built.
	@# (libbaresip.a is already staged by the underlying build rule)
	@touch "$(BARESIP_LIB).alsa"

# Stage the baresip executable built by the baresip CMake project.
# (This is separate from libbaresip.a which is the static library used by CGO.)
$(BARESIP_EXE): $(BARESIP_LIB)
	@set -eu; \
	if [ -f "$(BARESIP_DIR)/build/baresip" ]; then \
	  cp "$(BARESIP_DIR)/build/baresip" "$(BARESIP_EXE)"; \
	elif [ -f "$(BARESIP_DIR)/build/src/baresip" ]; then \
	  cp "$(BARESIP_DIR)/build/src/baresip" "$(BARESIP_EXE)"; \
	else \
	  echo "ERROR: baresip executable not found after CMake build." >&2; \
	  echo "Searched: $(BARESIP_DIR)/build/baresip, $(BARESIP_DIR)/build/src/baresip" >&2; \
	  exit 1; \
	fi

###############################################################################
# Header staging
###############################################################################
stage_headers: $(HDR_STAGE_RE) $(HDR_STAGE_REM) $(HDR_STAGE_BARESIP)

$(HDR_STAGE_RE) $(HDR_STAGE_REM) $(HDR_STAGE_BARESIP): $(BARESIP_DIR) $(RE_DIR) $(REM_DIR) | $(GIT_DIR)
	@set -eu; \
	echo "stage_headers: staging headers into $(LIBBARESIP_DIR)/{re,rem,baresip}/include"; \
	rm -rf "$(STAGE_RE)/include" "$(STAGE_REM)/include" "$(STAGE_BARESIP)/include" 2>/dev/null || true; \
	mkdir -p "$(STAGE_RE)" "$(STAGE_REM)" "$(STAGE_BARESIP)"; \
	if [ -d "$(RE_DIR)/include" ]; then cp -a "$(RE_DIR)/include" "$(STAGE_RE)/" 2>/dev/null || true; else echo "stage_headers: missing $(RE_DIR)/include" >&2; fi; \
	if [ -d "$(REM_DIR)/include" ]; then cp -a "$(REM_DIR)/include" "$(STAGE_REM)/" 2>/dev/null || true; else echo "stage_headers: missing $(REM_DIR)/include" >&2; fi; \
	if [ -d "$(BARESIP_DIR)/include" ]; then cp -a "$(BARESIP_DIR)/include" "$(STAGE_BARESIP)/" 2>/dev/null || true; else echo "stage_headers: missing $(BARESIP_DIR)/include" >&2; fi; \
	test -f "$(STAGE_RE)/include/re.h"; \
	test -f "$(STAGE_REM)/include/rem.h"; \
	test -f "$(STAGE_BARESIP)/include/baresip.h"

###############################################################################
# Cleanup
###############################################################################
test:
	$(call check_alsa,noalsa)
	@echo "Running tests..."
	go test -race ./...

test_alsa:
	$(call check_alsa,alsa)
	@echo "Running tests with ALSA..."
	go test -tags alsa -race ./...

clean:
	@rm -rf $(STAGE_RE) $(STAGE_REM) $(STAGE_BARESIP) $(STAGE_G722)
	@rm -rf $(RE_DIR) $(REM_DIR) $(BARESIP_DIR)
	@rm -f telefonist $(ALSA_MARKER)
	@echo "Cleaned project sources and build artifacts. (Third-party libs like OpenSSL preserved)"

distclean:
	@rm -rf "$(LIBBARESIP_DIR)"
	@rm -f telefonist
	@echo "Removed EVERYTHING in $(LIBBARESIP_DIR)"
	@true
