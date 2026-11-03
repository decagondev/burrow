// qthello.cpp - QtCore on its own (W15-S01, S02): a QCoreApplication that
// prints, runs a worker on a QThread whose signal comes back to the main
// thread through the event loop (moc, queued connections), fires a QTimer,
// reads a file with QFile, lists its directory with QDir, runs /bin/echo
// with QProcess (fork and exec), asks QRandomGenerator and QDateTime, and
// exits. Prints "qthello: <step> ok" lines, then "qthello: ok" and the
// smoke marker "[qt] core ok"; a failed step prints FAIL and exits 1.
// QTHELLO_ECHO names another echo (the census runs Burrow's static one on
// the host). Uses nothing Burrow-specific: it runs on Linux first.
// Docs: docs/04-plan/waves/WAVE-15-qt.md.
#include <QCoreApplication>
#include <QDateTime>
#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QProcess>
#include <QRandomGenerator>
#include <QThread>
#include <QTimer>
#include <cstdio>

static int failures;

static void check(const char *step, bool good) {
    std::printf("qthello: %s %s\n", step, good ? "ok" : "FAIL");
    std::fflush(stdout);
    if (!good) failures++;
}

// Worker sums 1..n on its own thread and reports back with a signal.
class Worker : public QObject {
    Q_OBJECT
public:
    explicit Worker(int n) : n(n) {}
signals:
    void done(long long sum, bool onWorkerThread);
public slots:
    void run() {
        long long s = 0;
        for (int i = 1; i <= n; i++) s += i;
        emit done(s, QThread::currentThread() != QCoreApplication::instance()->thread());
    }

private:
    int n;
};

int main(int argc, char **argv) {
    QCoreApplication app(argc, argv);
    std::printf("qthello: Qt %s\n", qVersion());
    QFile self(QCoreApplication::applicationFilePath());
    check("file", self.open(QIODevice::ReadOnly) && self.read(4) == QByteArray("\x7f" "ELF") &&
                      self.size() == QFileInfo(self).size());
    quint32 a = QRandomGenerator::system()->generate(), b = QRandomGenerator::system()->generate();
    check("random", a != b);
    check("clock", QDateTime::currentDateTimeUtc().date().year() >= 2026);
    check("dir", QDir(QCoreApplication::applicationDirPath()).entryList(QDir::Files).contains("qthello"));
    QProcess echo;
    echo.start(qEnvironmentVariable("QTHELLO_ECHO", "/bin/echo"), {"hello from a child"});
    bool finished = echo.waitForFinished(5000);
    QByteArray out = echo.readAllStandardOutput();
    bool good = finished && echo.exitStatus() == QProcess::NormalExit && echo.exitCode() == 0 &&
                out == "hello from a child\n";
    if (!good)
        std::printf("qthello: process: finished %d, %s, exit %d, output \"%s\"\n", finished,
                    qPrintable(echo.errorString()), echo.exitCode(), out.constData());
    check("process", good);

    QThread thread;
    Worker worker(100000);
    worker.moveToThread(&thread);
    QObject::connect(&thread, &QThread::started, &worker, &Worker::run);
    QObject::connect(&worker, &Worker::done, &app, [&](long long sum, bool elsewhere) {
        check("thread", sum == 5000050000LL && elsewhere &&
                            QThread::currentThread() == app.thread());
        thread.quit();
        QTimer::singleShot(50, &app, [&] {
            check("timer", true);
            app.quit();
        });
    });
    thread.start();
    QTimer::singleShot(10000, &app, [&] { check("finished in time", false); app.quit(); });
    app.exec();
    thread.wait();
    if (failures) {
        std::printf("qthello: %d FAILED\n", failures);
        return 1;
    }
    std::printf("qthello: ok\n[qt] core ok\n");
    return 0;
}

#include "qthello.moc"
