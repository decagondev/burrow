# user/qt - Burrow's Qt programs (W15), built by scripts/qt.sh against the
# static musl qtbase: qthello (QtCore only) and qtwindow (QtGui, raster).
# Static Qt links only the plugins named here: linuxfb draws on Burrow,
# offscreen lets the host check the same binary without a screen.
# Docs: docs/04-plan/waves/WAVE-15-qt.md.
cmake_minimum_required(VERSION 3.25)
project(burrow_qt LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
find_package(Qt6 REQUIRED COMPONENTS Core Gui)
qt_standard_project_setup()

qt_add_executable(qthello qthello.cpp)
target_link_libraries(qthello PRIVATE Qt6::Core)

qt_add_executable(qtwindow qtwindow.cpp)
target_link_libraries(qtwindow PRIVATE Qt6::Gui)
qt_import_plugins(qtwindow INCLUDE Qt6::QLinuxFbIntegrationPlugin Qt6::QOffscreenIntegrationPlugin)
