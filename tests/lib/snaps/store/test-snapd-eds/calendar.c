#include <stdio.h>
#include <string.h>

#include <glib.h>
#include <libecal/libecal.h>

G_DEFINE_AUTOPTR_CLEANUP_FUNC(ECalClient, g_object_unref)
G_DEFINE_AUTOPTR_CLEANUP_FUNC(ECalComponent, g_object_unref)
G_DEFINE_AUTOPTR_CLEANUP_FUNC(ESource, g_object_unref)
G_DEFINE_AUTOPTR_CLEANUP_FUNC(ESourceRegistry, g_object_unref)


struct open_data {
    GMainLoop *main_loop;
    const char *source_id;
    GError **error;
    ECalClient **calendar;
    gboolean should_quit;
};

static void
source_added(ESourceRegistry *registry, ESource *source, gpointer user_data)
{
    struct open_data *data = user_data;

    // Ignore sources with the wrong ID
    if (g_strcmp0(e_source_get_uid(source), data->source_id) != 0)
        return;

    *data->calendar = (ECalClient*)e_cal_client_connect_sync(
        source, E_CAL_CLIENT_SOURCE_TYPE_EVENTS, 30, NULL, data->error);

    if (data->should_quit)
        g_main_loop_quit(data->main_loop);
}

static gboolean
source_added_timeout(gpointer user_data)
{
    struct open_data *data = user_data;

    g_set_error_literal(data->error, G_IO_ERROR, G_IO_ERROR_TIMED_OUT,
        "Timed out while waiting for ESource creation from the registry");

    if (data->should_quit)
        g_main_loop_quit(data->main_loop);
    // open_or_create removes us
    return G_SOURCE_CONTINUE;
}

static ECalClient *
open_or_create (ESourceRegistry *registry, const char *source_id,
                GError **error)
{
    g_autoptr(GMainLoop) main_loop = NULL;
    g_autoptr(ECalClient) calendar = NULL;
    g_autoptr(ESource) scratch = NULL;
    g_autoptr(GError) commit_error = NULL;

    main_loop = g_main_loop_new (NULL, FALSE);
    // Listen to registry for added sources
    struct open_data data = {
        .main_loop = main_loop,
        .source_id = source_id,
        .error = error,
        .calendar = &calendar,
        .should_quit = FALSE,
    };
    guint source_added_id = g_signal_connect(registry, "source-added",
                                             G_CALLBACK(source_added), &data);

    // Create a new local calendar with the desired source ID
    scratch = e_source_new_with_uid(source_id, NULL, error);
    if (!scratch)
        goto end;
    e_source_set_display_name(scratch, source_id);
    ESourceBackend *backend = e_source_get_extension(
        scratch, E_SOURCE_EXTENSION_CALENDAR);
    e_source_backend_set_backend_name(backend, "local");

    // Try to commit the new source to the registry, which will fail
    // if it already exists
    if (!e_source_registry_commit_source_sync(
            registry, scratch, NULL, &commit_error)) {
        if (g_error_matches(commit_error, G_IO_ERROR, G_IO_ERROR_EXISTS)) {
            g_autoptr(ESource) source = e_source_registry_ref_source(
                registry, source_id);
            source_added(registry, source, &data);
        } else {
            g_propagate_error(error, commit_error);
            commit_error = NULL;
            goto end;
        }
    }

    // If we don't have the calendar at this point, wait on the
    // source-added signal for it to be created.  Set a timer so we
    // don't wait forever.
    if (!calendar) {
        guint timeout_id = g_timeout_add_seconds(
            20, source_added_timeout, &data);
        data.should_quit = TRUE;
        g_main_loop_run (main_loop);
        g_source_remove(timeout_id);
    }

end:
    if (source_added_id) {
        g_signal_handler_disconnect(registry, source_added_id);
    }
    return g_steal_pointer(&calendar);
}

static gboolean
load_event_from_stdin(ECalClient *calendar, GError **error)
{
    g_autoptr(GString) ics_data = g_string_new(NULL);
    char buffer[4092];
    size_t n_read = fread(buffer, 1, sizeof(buffer), stdin);
    while (n_read > 0) {
        g_string_append_len(ics_data, buffer, n_read);
        n_read = fread(buffer, 1, sizeof(buffer), stdin);
    }

    g_autoptr(ECalComponent) component = e_cal_component_new_from_string(ics_data->str);
    if (!component) {
        g_set_error_literal(error, G_IO_ERROR, G_IO_ERROR_INVALID_DATA,
                            "could not parse iCalendar data");
        return FALSE;
    }

    return e_cal_client_create_object_sync(
        calendar, e_cal_component_get_icalcomponent(component), NULL,
        NULL, error);
}

static gboolean
list_events(ECalClient *calendar, GError **error)
{
    GSList *results = NULL;
    if (!e_cal_client_get_object_list_as_comps_sync(calendar, "#t", &results,
                                                    NULL, error)) {
        return FALSE;
    }

    const GSList *l;
    for (l = results; l != NULL; l = l->next) {
        ECalComponent *component = l->data;
        g_autofree char *ical = e_cal_component_get_as_string(component);
        g_print("%s\n", ical);
    }
    e_cal_client_free_icalcomp_slist(results);
    return TRUE;
}

static gboolean
remove_calendar(ECalClient *calendar, GError **error)
{
    return e_client_remove_sync(E_CLIENT(calendar), NULL, error);
}

int main(int argc, char **argv)
{
    g_autoptr(GError) error = NULL;
    g_autoptr(ESourceRegistry) registry = NULL;
    g_autoptr(ECalClient) calendar = NULL;

    if (argc != 3 || !(!strcmp(argv[1], "load") ||
                       !strcmp(argv[1], "list") ||
                       !strcmp(argv[1], "remove"))) {
        g_printerr("usage: calendar {load|list|remove} CALENDAR-ID\n");
        return 1;
    }

    // Connect to the EDS registry service
    registry = e_source_registry_new_sync(NULL, &error);
    if (!registry) {
        goto end;
    }

    calendar = open_or_create(registry, argv[2], &error);
    if (!calendar) {
        goto end;
    }

    if (!strcmp(argv[1], "load")) {
        load_event_from_stdin(calendar, &error);
    } else if (!strcmp(argv[1], "list")) {
        list_events(calendar, &error);
    } else if (!strcmp(argv[1], "remove")) {
        remove_calendar(calendar, &error);
    }

end:
    if (error) {
        g_printerr("error: %s[%d] %s\n", g_quark_to_string(error->domain),
                   error->code, error->message);
        return 1;
    }
    return 0;
}
