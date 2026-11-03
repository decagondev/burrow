// qtpad.cpp - a small Qt text editor for Burrow (W15-S06): "qtpad <file>"
// opens a file (or starts it), in DejaVu Sans Mono with line numbers and C
// or Lua colouring (highlight.h). Ctrl+S saves (QSaveFile: a new file,
// then a rename), Ctrl+Shift+S saves under another name, Ctrl+F finds and
// F3 finds the next, Ctrl+Q quits (twice with unsaved changes). Every
// action prints a "qtpad:" line, which the smoke test waits for. Never
// writes the file but on a save. Docs: docs/04-plan/waves/WAVE-15-qt.md,
// docs/06-guides/WORKBENCH.md.
#include "highlight.h"
#include <QApplication>
#include <QFile>
#include <QInputDialog>
#include <QMainWindow>
#include <QPainter>
#include <QPlainTextEdit>
#include <QSaveFile>
#include <QStatusBar>
#include <QTextBlock>
#include <cstdio>

static void say(const QString &s) {
    std::printf("qtpad: %s\n", qPrintable(s));
    std::fflush(stdout);
}

// Editor is a QPlainTextEdit with a line-number gutter on its left.
class Editor : public QPlainTextEdit {
public:
    Editor() : gutter(new Gutter(this)) {
        connect(this, &QPlainTextEdit::blockCountChanged, this, [this] { setViewportMargins(gutterWidth(), 0, 0, 0); });
        connect(this, &QPlainTextEdit::updateRequest, this, [this](const QRect &r, int dy) {
            dy ? gutter->scroll(0, dy) : gutter->update(0, r.y(), gutter->width(), r.height());
        });
        setViewportMargins(gutterWidth(), 0, 0, 0);
    }
    int gutterWidth() const {
        return 12 + fontMetrics().horizontalAdvance('9') * QString::number(qMax(1, blockCount())).size();
    }

protected:
    void resizeEvent(QResizeEvent *e) override {
        QPlainTextEdit::resizeEvent(e);
        QRect cr = contentsRect();
        gutter->setGeometry(cr.left(), cr.top(), gutterWidth(), cr.height());
    }

private:
    class Gutter : public QWidget {
    public:
        explicit Gutter(Editor *e) : QWidget(e), ed(e) {}

    protected:
        void paintEvent(QPaintEvent *ev) override {
            QPainter p(this);
            p.fillRect(ev->rect(), QColor(0xEE, 0xEE, 0xEE));
            p.setPen(QColor(0x88, 0x88, 0x88));
            QTextBlock b = ed->firstVisibleBlock();
            int top = int(ed->blockBoundingGeometry(b).translated(ed->contentOffset()).top());
            for (; b.isValid() && top <= ev->rect().bottom(); b = b.next()) {
                int h = int(ed->blockBoundingRect(b).height());
                if (b.isVisible())
                    p.drawText(0, top, width() - 6, h, Qt::AlignRight, QString::number(b.blockNumber() + 1));
                top += h;
            }
        }

    private:
        Editor *ed;
    };
    Gutter *gutter;
};

class Pad : public QMainWindow {
public:
    explicit Pad(const QString &path) : file(path) {
        setCentralWidget(&edit);
        QFont mono("DejaVu Sans Mono");
        mono.setPixelSize(18);
        edit.setFont(mono);
        open(path);
        addShortcut("Ctrl+S", [this] { save(file); });
        addShortcut("Ctrl+Shift+S", [this] { saveAs(); });
        addShortcut("Ctrl+F", [this] { find(true); });
        addShortcut("F3", [this] { find(false); });
        addShortcut("Ctrl+Q", [this] { quit(); });
        connect(edit.document(), &QTextDocument::modificationChanged, this, [this] { showName(); });
    }

private:
    template <typename F> void addShortcut(const char *keys, F f) {
        auto *a = new QAction(this);
        a->setShortcut(QKeySequence(keys));
        connect(a, &QAction::triggered, this, f);
        addAction(a);
    }
    void open(const QString &path) {
        QFile f(path);
        bool exists = f.open(QIODevice::ReadOnly);
        edit.setPlainText(exists ? QString::fromUtf8(f.readAll()) : QString());
        new Highlighter(edit.document(), Highlighter::forFile(path));
        edit.document()->setModified(false);
        showName();
        say(QString("%1 %2 (%3 lines)").arg(exists ? "opened" : "new file").arg(path).arg(edit.blockCount()));
    }
    void save(const QString &path) {
        QSaveFile f(path);
        QByteArray data = edit.toPlainText().toUtf8();
        if (!f.open(QIODevice::WriteOnly) || f.write(data) != data.size() || !f.commit()) {
            statusBar()->showMessage("could not save " + path + ": " + f.errorString());
            say(QString("could not save %1: %2").arg(path, f.errorString()));
            return;
        }
        file = path;
        edit.document()->setModified(false);
        statusBar()->showMessage("saved " + path, 3000);
        say(QString("saved %1 (%2 bytes)").arg(path).arg(data.size()));
    }
    void saveAs() {
        bool ok;
        QString to = QInputDialog::getText(this, "Save as", "File:", QLineEdit::Normal, file, &ok);
        if (ok && !to.isEmpty()) save(to);
    }
    void find(bool ask) {
        if (ask || needle.isEmpty()) {
            bool ok;
            QString s = QInputDialog::getText(this, "Find", "Find:", QLineEdit::Normal, needle, &ok);
            if (!ok || s.isEmpty()) return;
            needle = s;
        }
        bool found = edit.find(needle) || (edit.moveCursor(QTextCursor::Start), edit.find(needle));
        say(QString("%1 \"%2\" at line %3").arg(found ? "found" : "did not find").arg(needle)
                .arg(edit.textCursor().blockNumber() + 1));
    }
    void quit() {
        if (edit.document()->isModified() && !warned) {
            warned = true;
            statusBar()->showMessage("unsaved changes: Ctrl+S saves, Ctrl+Q again quits");
            say("unsaved changes");
            return;
        }
        say("quit");
        QApplication::quit();
    }
    void showName() {
        setWindowTitle(file + (edit.document()->isModified() ? " *" : ""));
        statusBar()->showMessage(windowTitle());
    }

    Editor edit;
    QString file, needle;
    bool warned = false;
};

int main(int argc, char **argv) {
    QApplication app(argc, argv);
    if (argc < 2) {
        std::fprintf(stderr, "usage: qtpad <file>\n");
        return 2;
    }
    QFont font("DejaVu Sans");
    font.setPixelSize(16);
    app.setFont(font);
    Pad pad(QString::fromLocal8Bit(argv[1]));
    pad.showFullScreen();
    return app.exec();
}
