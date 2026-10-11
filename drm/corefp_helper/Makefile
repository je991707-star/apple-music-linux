# Makefile for corefp_helper — Wine subprocess helper for CoreFP.dll
#
# Produces:
#   corefp_helper.exe     — shell wrapper (invokes wine with the .so)
#   corefp_helper.exe.so  — ELF shared object loaded by Wine
#
# Requirements: wine, winegcc (pacman -S wine  /  apt install wine)

WINEGCC  ?= winegcc
CFLAGS   := -O2 -Wall -Wextra -Werror
TARGET   := corefp_helper.exe
SO       := corefp_helper.exe.so

.PHONY: all clean

all: $(TARGET)

$(TARGET): corefp_helper.c
	$(WINEGCC) $(CFLAGS) -o corefp_helper corefp_helper.c
	@# winegcc names outputs <stem>.exe / <stem>.exe.so when stem has no extension;
	@# normalise to our expected names.
	@if [ -f corefp_helper.exe ] && [ ! -f $(TARGET) ]; then \
	    mv corefp_helper.exe $(TARGET); fi
	@if [ -f corefp_helper.exe.so ] && [ ! -f $(SO) ]; then \
	    mv corefp_helper.exe.so $(SO); fi

clean:
	rm -f $(TARGET) $(SO) corefp_helper.exe corefp_helper.exe.so
