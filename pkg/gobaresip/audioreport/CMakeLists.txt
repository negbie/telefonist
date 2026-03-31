cmake_minimum_required(VERSION 3.10)

# This CMakeLists.txt is intended to be used when this directory is copied into:
#   baresip/modules/audioreport
#
# Match baresip-apps module pattern:
# - register module name into MODULES_DETECTED so parent can pick it up
# - build as OBJECT when STATIC is enabled, else as a loadable MODULE (.so)

project(audioreport C)

list(APPEND MODULES_DETECTED ${PROJECT_NAME})
set(MODULES_DETECTED ${MODULES_DETECTED} PARENT_SCOPE)

set(SRCS
  audioreport.c
)

if(STATIC)
  add_library(${PROJECT_NAME} OBJECT ${SRCS})
else()
  add_library(${PROJECT_NAME} MODULE ${SRCS})
endif()
