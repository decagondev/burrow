// cxxtest.cpp - C++ on Burrow (W12-S05): a static musl build with
// libstdc++ that throws and catches exceptions across frames (the unwinder
// reads .eh_frame), runs std::thread workers with a std::mutex and a
// std::condition_variable, uses the standard containers and iostreams, and
// prints "cxxtest: ok" when every check holds. Built by scripts/ports.sh
// with the musl cross toolchain. Docs: docs/04-plan/waves/WAVE-12-ports.md.
#include <condition_variable>
#include <iostream>
#include <map>
#include <memory>
#include <mutex>
#include <numeric>
#include <stdexcept>
#include <string>
#include <thread>
#include <vector>

namespace {

struct Tracker { // counts destructions during unwinding
    static int alive;
    Tracker() { ++alive; }
    ~Tracker() { --alive; }
};
int Tracker::alive = 0;

int deep(int n) {
    Tracker t;
    if (n == 0) throw std::runtime_error("thrown 5 frames down");
    return deep(n - 1) + 1;
}

bool check(bool ok, const std::string &what) {
    std::cout << "cxxtest: " << what << "\n";
    return ok;
}

} // namespace

int main() {
    bool ok = true;

    std::string caught;
    try {
        deep(4);
    } catch (const std::exception &e) {
        caught = e.what();
    }
    ok &= check(caught == "thrown 5 frames down" && Tracker::alive == 0,
                "exception caught: \"" + caught + "\", " + std::to_string(Tracker::alive) + " objects left");

    std::mutex mu;
    std::condition_variable cv;
    long total = 0;
    int finished = 0;
    std::vector<std::thread> workers;
    for (int w = 0; w < 4; w++) {
        workers.emplace_back([&, w] {
            std::vector<long> part(1000);
            std::iota(part.begin(), part.end(), 1000L * w + 1);
            long sum = std::accumulate(part.begin(), part.end(), 0L);
            std::lock_guard<std::mutex> lock(mu);
            total += sum;
            finished++;
            cv.notify_one();
        });
    }
    {
        std::unique_lock<std::mutex> lock(mu);
        cv.wait(lock, [&] { return finished == 4; });
    }
    for (auto &t : workers) t.join();
    ok &= check(total == 4000L * 4001 / 2, "4 std::threads summed 1..4000: " + std::to_string(total));

    std::exception_ptr fromThread;
    std::thread([&] {
        try {
            throw std::logic_error("from a thread");
        } catch (...) {
            fromThread = std::current_exception();
        }
    }).join();
    try {
        std::rethrow_exception(fromThread);
    } catch (const std::logic_error &e) {
        ok &= check(std::string(e.what()) == "from a thread", std::string("rethrown on main: ") + e.what());
    }

    std::map<std::string, int> words;
    for (auto w : {"burrow", "go", "burrow", "doom", "go", "burrow"}) words[w]++;
    auto owned = std::make_unique<std::vector<int>>(10, 7);
    ok &= check(words["burrow"] == 3 && owned->size() == 10,
                "map counted burrow " + std::to_string(words["burrow"]) + " times");

    std::cout << (ok ? "cxxtest: ok" : "cxxtest: FAILED") << std::endl;
    return ok ? 0 : 1;
}
