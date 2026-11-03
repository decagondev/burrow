// highlight.h - qtpad's syntax colouring (W15-S06): keywords, strings,
// numbers and comments for C and Lua, chosen by the file's extension. A
// QSyntaxHighlighter with regular expressions; block comments carry over
// lines with the block state. Never parses: colours are a reading aid.
// Docs: docs/04-plan/waves/WAVE-15-qt.md.
#pragma once
#include <QRegularExpression>
#include <QSyntaxHighlighter>
#include <QTextCharFormat>
#include <vector>

class Highlighter : public QSyntaxHighlighter {
public:
    enum Language { None, C, Lua };
    Highlighter(QTextDocument *doc, Language lang);
    static Language forFile(const QString &path);

protected:
    void highlightBlock(const QString &text) override;

private:
    struct Rule {
        QRegularExpression re;
        QTextCharFormat format;
    };
    std::vector<Rule> rules;
    QRegularExpression commentStart, commentEnd;
    QTextCharFormat comment;
};
