#import <AppKit/AppKit.h>
#include <stdlib.h>
#include <string.h>

// Write s as the clipboard string and mark it concealed so clipboard managers
// and Universal Clipboard skip it (spec §9).
void harmosClipWrite(const char* s) {
    @autoreleasepool {
        NSPasteboard* pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        NSString* str = [NSString stringWithUTF8String:s];
        if (str != nil) {
            [pb setString:str forType:NSPasteboardTypeString];
        }
        [pb setString:@"" forType:@"org.nspasteboard.ConcealedType"];
    }
}

// Read the clipboard string. Returns a heap copy the Go side frees.
char* harmosClipRead(void) {
    @autoreleasepool {
        NSPasteboard* pb = [NSPasteboard generalPasteboard];
        NSString* s = [pb stringForType:NSPasteboardTypeString];
        if (s == nil) {
            return strdup("");
        }
        const char* u = [s UTF8String];
        return strdup(u ? u : "");
    }
}

void harmosClipClear(void) {
    @autoreleasepool {
        [[NSPasteboard generalPasteboard] clearContents];
    }
}
