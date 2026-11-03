// qtbench.cpp - how fast Qt is on Burrow (W15-S05). Run as
// "stopwatch /opt/qt/bin/qtbench": QTBENCH_T0 is when the exec began. It
// reports the time to main and to the first full-screen frame, the time
// per full-screen repaint (30 frames, each a new colour and a line of
// text), the memory the system lost meanwhile (sysinfo), and how often the
// event loop wakes in 2 s of idling (each wake is a return from ppoll: an
// idle program should not wake at all). Prints "qtbench: <name> <value>"
// lines, then "qtbench: done". make qt-bench checks them against budgets.
// Docs: docs/04-plan/waves/WAVE-15-qt.md.
#include <QAbstractEventDispatcher>
#include <QElapsedTimer>
#include <QFont>
#include <QGuiApplication>
#include <QPainter>
#include <QRasterWindow>
#include <QTimer>
#include <cstdio>
#include <cstdlib>
#include <ctime>
#include <sys/sysinfo.h>

static constexpr int kFrames = 30;
static constexpr int kIdleMs = 2000;

static double msSinceExec() {
    timespec t;
    clock_gettime(CLOCK_MONOTONIC, &t);
    const char *t0 = std::getenv("QTBENCH_T0");
    long long now = (long long)t.tv_sec * 1000000000LL + t.tv_nsec;
    return t0 ? (now - std::atoll(t0)) / 1e6 : -1;
}

static long long freeBytes() {
    struct sysinfo si;
    sysinfo(&si);
    return (long long)si.freeram * si.mem_unit;
}

static void report(const char *name, double value, const char *unit) {
    std::printf("qtbench: %s %.1f %s\n", name, value, unit);
    std::fflush(stdout);
}

class Bench : public QRasterWindow {
public:
    long long free0 = 0;

protected:
    void paintEvent(QPaintEvent *) override {
        QPainter p(this);
        p.fillRect(QRect(QPoint(), size()), QColor::fromHsv((frames * 12) % 360, 160, 200));
        QFont f("DejaVu Sans");
        f.setPixelSize(40);
        p.setFont(f);
        p.drawText(QRect(QPoint(), size()), Qt::AlignCenter, QString("frame %1").arg(frames));
        if (frames++ == 0) {
            QTimer::singleShot(0, this, [this] { // after the first flush
                report("first-frame-ms", msSinceExec(), "ms");
                clock.start();
                update();
            });
            return;
        }
        if (frames <= kFrames) {
            update();
            return;
        }
        if (frames == kFrames + 1) {
            report("repaint-ms", clock.elapsed() / double(kFrames), "ms");
            report("memory-mib", (free0 - freeBytes()) / 1048576.0, "MiB");
            QTimer::singleShot(100, this, [this] { idle(); });
        }
    }

private:
    void idle() {
        auto *d = QAbstractEventDispatcher::instance();
        auto c = connect(d, &QAbstractEventDispatcher::awake, this, [this] { wakes++; });
        QTimer::singleShot(kIdleMs, this, [this, c] {
            disconnect(c);
            report("idle-wakes", wakes - 1, "in 2 s"); // the timer that ends the wait wakes it once
            std::printf("qtbench: done\n");
            std::fflush(stdout);
            QGuiApplication::quit();
        });
    }
    int frames = 0, wakes = 0;
    QElapsedTimer clock;
};

int main(int argc, char **argv) {
    long long free0 = freeBytes();
    report("main-ms", msSinceExec(), "ms");
    QGuiApplication app(argc, argv);
    Bench w;
    w.free0 = free0;
    w.showFullScreen();
    return app.exec();
}
