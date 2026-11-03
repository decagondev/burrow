// qtwindow.cpp - QtGui's raster painting (W15-S01, S03): a full-screen
// QRasterWindow with a dark background, three rectangles (red, green,
// blue) and a line of DejaVu Sans text. On Burrow it draws through
// linuxfb on /dev/fb0; on the host, with QT_QPA_PLATFORM=offscreen,
// "--png <file>" paints the same picture into an image and saves it.
// "--quit-after <ms>" ends it (default: run until Esc or q). Prints
// "qtwindow: shown WxH" once painted. Docs: docs/04-plan/waves/WAVE-15-qt.md.
#include <QFont>
#include <QGuiApplication>
#include <QImage>
#include <QKeyEvent>
#include <QPainter>
#include <QRasterWindow>
#include <QScreen>
#include <QTimer>
#include <cstdio>

// paint draws the picture; tests/smoke/expect/qt*.screen samples it.
static void paint(QPainter &p, const QSize &size) {
    p.fillRect(QRect(QPoint(0, 0), size), QColor(0x20, 0x24, 0x30));
    p.fillRect(100, 100, 200, 150, QColor(0xE0, 0x30, 0x30));
    p.fillRect(350, 100, 200, 150, QColor(0x30, 0xC0, 0x50));
    p.fillRect(600, 100, 200, 150, QColor(0x30, 0x60, 0xE0));
    QFont f("DejaVu Sans");
    f.setPixelSize(48);
    p.setFont(f);
    p.setPen(Qt::white);
    p.drawText(QRect(100, 320, size.width() - 200, 80), Qt::AlignLeft | Qt::AlignVCenter, "Burrow + Qt");
}

class Window : public QRasterWindow {
protected:
    void paintEvent(QPaintEvent *) override {
        QPainter p(this);
        paint(p, size());
        if (!shown) {
            shown = true;
            std::printf("qtwindow: shown %dx%d\n", width(), height());
            std::fflush(stdout);
        }
    }
    void keyPressEvent(QKeyEvent *e) override {
        if (e->key() == Qt::Key_Escape || e->key() == Qt::Key_Q) QGuiApplication::quit();
    }

private:
    bool shown = false;
};

int main(int argc, char **argv) {
    QGuiApplication app(argc, argv);
    QString png;
    int quitAfter = -1;
    for (int i = 1; i + 1 < argc; i++) {
        if (QByteArray(argv[i]) == "--png") png = argv[++i];
        else if (QByteArray(argv[i]) == "--quit-after") quitAfter = QByteArray(argv[++i]).toInt();
    }
    QSize size = app.primaryScreen()->size();
    if (!png.isEmpty()) {
        QImage img(size.isEmpty() ? QSize(1024, 768) : size, QImage::Format_RGB32);
        QPainter p(&img);
        paint(p, img.size());
        p.end();
        bool saved = img.save(png);
        std::printf("qtwindow: %s %s\n", saved ? "saved" : "could not save", qPrintable(png));
        return saved ? 0 : 1;
    }
    Window w;
    w.showFullScreen();
    if (quitAfter >= 0) QTimer::singleShot(quitAfter, &app, &QGuiApplication::quit);
    return app.exec();
}
