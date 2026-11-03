// qtdemo.cpp - Qt widgets driven by the keyboard and mouse (W15-S04): a
// full-screen QMainWindow with a File menu (Clear, Quit on Ctrl+Q), a line
// edit, an Add button and a list. Add (or Enter) appends the line's text to
// the list. On Burrow, Qt's evdev handlers read /dev/input/event0 (the
// keyboard, grabbed) and event1 (the mouse, with Qt's drawn cursor).
// Widgets sit at fixed places (no layouts), so a smoke test can aim the
// mouse; each step prints a "qtdemo:" line on stdout for the serial log.
// Docs: docs/04-plan/waves/WAVE-15-qt.md.
#include <QApplication>
#include <QKeySequence>
#include <QLineEdit>
#include <QListWidget>
#include <QMainWindow>
#include <QMenu>
#include <QMenuBar>
#include <QMouseEvent>
#include <QPushButton>
#include <QScreen>
#include <QTimer>
#include <cstdio>

// The places tests/smoke/keys/qt.keys aims at.
static const QRect editRect(40, 80, 400, 40), addRect(460, 80, 140, 40), listRect(40, 140, 560, 400);

static void say(const QString &s) {
    std::printf("qtdemo: %s\n", qPrintable(s));
    std::fflush(stdout);
}

// Presses reports where each mouse press lands (a test that misses its
// target says by how much).
class Presses : public QObject {
protected:
    bool eventFilter(QObject *o, QEvent *e) override {
        if (e->type() == QEvent::MouseButtonPress && o->isWindowType()) {
            QPointF p = static_cast<QMouseEvent *>(e)->globalPosition();
            say(QString("press at %1,%2").arg(int(p.x())).arg(int(p.y())));
        }
        return false;
    }
};

int main(int argc, char **argv) {
    QApplication app(argc, argv);
    Presses presses;
    app.installEventFilter(&presses);
    QFont font("DejaVu Sans");
    font.setPixelSize(18);
    app.setFont(font);

    QMainWindow win;
    QWidget *central = new QWidget;
    win.setCentralWidget(central);
    auto *edit = new QLineEdit(central);
    auto *add = new QPushButton("Add", central);
    auto *list = new QListWidget(central);
    edit->setGeometry(editRect);
    add->setGeometry(addRect);
    list->setGeometry(listRect);
    add->setFocusPolicy(Qt::NoFocus); // a click on Add leaves the typing in the line edit
    list->setFocusPolicy(Qt::NoFocus);

    QMenu *file = win.menuBar()->addMenu("&File");
    file->addAction("Clear", list, &QListWidget::clear);
    QAction *quit = file->addAction("Quit", &app, &QApplication::quit);
    quit->setShortcut(QKeySequence("Ctrl+Q"));
    win.addAction(quit); // the shortcut works with the menu closed

    auto append = [=] {
        if (edit->text().isEmpty()) return;
        list->addItem(edit->text());
        say(QString("added \"%1\" (%2 items)").arg(edit->text()).arg(list->count()));
        edit->clear();
    };
    QObject::connect(add, &QPushButton::clicked, append);
    QObject::connect(edit, &QLineEdit::returnPressed, append);
    QObject::connect(edit, &QLineEdit::textEdited, [](const QString &t) {
        if (t.endsWith(' ') || t.size() % 4 == 0) say(QString("typed \"%1\"").arg(t));
    });
    QObject::connect(file, &QMenu::aboutToShow, [] { say("menu open"); });
    QObject::connect(file, &QMenu::aboutToHide, [] { say("menu closed"); });
    QObject::connect(&app, &QApplication::aboutToQuit, [] { say("quit"); });

    win.showFullScreen();
    edit->setFocus();
    QTimer::singleShot(0, [&] {
        QPoint at = win.menuBar()->actionGeometry(file->menuAction()).center();
        QSize s = app.primaryScreen()->size();
        QPoint b = add->mapToGlobal(add->rect().center());
        say(QString("ready %1x%2, File at %3,%4, Add at %5,%6").arg(s.width()).arg(s.height()).arg(at.x()).arg(at.y())
                .arg(b.x()).arg(b.y()));
    });
    return app.exec();
}
