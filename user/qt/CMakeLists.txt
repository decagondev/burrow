# user/qt - Burrow's Qt programs (W15), built by scripts/qt.sh against the
# static musl qtbase: qthello (QtCore only) and qtwindow (QtGui, raster).
# Static Qt links only the plugins named here: linuxfb draws on Burrow,
# offscreen lets the host check the same binary without a screen. qtdemo
# (Widgets) is the keyboard and mouse demo (W15-S04).
# Docs: docs/04-plan/waves/WAVE-15-qt.md.
cmake_minimum_required(VERSION 3.25)
project(burrow_qt LANGUAGES CXX)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
find_package(Qt6 REQUIRED COMPONENTS Core Gui Widgets)
qt_standard_project_setup()

qt_add_executable(qthello qthello.cpp)
target_link_libraries(qthello PRIVATE Qt6::Core)

qt_add_executable(qtwindow qtwindow.cpp)
target_link_libraries(qtwindow PRIVATE Qt6::Gui)
qt_import_plugins(qtwindow INCLUDE Qt6::QLinuxFbIntegrationPlugin Qt6::QOffscreenIntegrationPlugin)

qt_add_executable(qtdemo qtdemo.cpp)
target_link_libraries(qtdemo PRIVATE Qt6::Widgets)
qt_import_plugins(qtdemo INCLUDE Qt6::QLinuxFbIntegrationPlugin Qt6::QOffscreenIntegrationPlugin)

qt_add_executable(qtpad qtpad.cpp highlight.cpp)
target_link_libraries(qtpad PRIVATE Qt6::Widgets)
qt_import_plugins(qtpad INCLUDE Qt6::QLinuxFbIntegrationPlugin Qt6::QOffscreenIntegrationPlugin)

qt_add_executable(qtbench qtbench.cpp)
target_link_libraries(qtbench PRIVATE Qt6::Gui)
qt_import_plugins(qtbench INCLUDE Qt6::QLinuxFbIntegrationPlugin Qt6::QOffscreenIntegrationPlugin)
